package preview

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"
)

// encodeJPEG は単色の小さな JPEG を作る。
func encodeJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return b.Bytes()
}

// 連結された JPEG ストリームを SOI/EOI で正しく分割できること。
func TestReadFrames_SplitsConcatenatedJPEG(t *testing.T) {
	j := encodeJPEG(t)
	if j[0] != 0xFF || j[1] != 0xD8 {
		t.Fatalf("期待した SOI で始まっていない")
	}
	// 先頭にゴミを付けても SOI から拾えること、3フレーム連結を確認。
	stream := append([]byte{0x00, 0x01}, j...)
	stream = append(stream, j...)
	stream = append(stream, j...)

	var got int
	p := &Preview{}
	p.readFrames(bytes.NewReader(stream), func(b []byte) {
		if _, err := jpeg.Decode(bytes.NewReader(b)); err != nil {
			t.Errorf("分割したフレームがデコードできない: %v", err)
		}
		got++
	})
	if got != 3 {
		t.Fatalf("フレーム数 = %d, 期待 3", got)
	}
}
