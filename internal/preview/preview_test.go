package preview

import (
	"bytes"
	"image"
	"image/jpeg"
	"sync"
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

// Start/Stop を並行に叩いても data race やリスナ/goroutine リークが無いこと
// （go test -race で検証）。bin は即終了する true を使い、接続は発生させない。
func TestStartStop_Concurrent(t *testing.T) {
	p := New(func() {})
	args := func(url string) []string { return []string{} }
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = p.Start("/usr/bin/true", args)
				p.Stop()
			}
		}()
	}
	wg.Wait()
	p.Stop()
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
