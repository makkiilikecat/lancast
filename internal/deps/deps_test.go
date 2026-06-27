package deps

import "testing"

const sampleEncoders = `Encoders:
 V..... = Video
 ------
 V....D libx264              libx264 H.264 / AVC
 V....D hevc_videotoolbox    VideoToolbox H.265 Encoder
 A....D aac                  AAC (Advanced Audio Coding)
`

func TestParseEncoders(t *testing.T) {
	got := ParseEncoders(sampleEncoders)
	for _, want := range []string{"libx264", "hevc_videotoolbox", "aac"} {
		if !got[want] {
			t.Errorf("エンコーダ %q が検出されない", want)
		}
	}
	// ヘッダ行や説明文を拾わないこと。
	if got["="] || got["Video"] || got["Encoders:"] {
		t.Errorf("非エンコーダ行を誤検出: %+v", got)
	}
}

func TestParseModuleLoaded(t *testing.T) {
	const procModules = `v4l2loopback 61440 1 - Live 0x0000000000000000
videodev 364544 2 v4l2loopback - Live 0x0000000000000000
`
	if !ParseModuleLoaded(procModules, "v4l2loopback") {
		t.Error("v4l2loopback が検出されない")
	}
	if ParseModuleLoaded(procModules, "nonexistent") {
		t.Error("存在しないモジュールを誤検出")
	}
}

func TestResultOK(t *testing.T) {
	all := Result{Checks: []Check{{OK: true}, {OK: true}}}
	if !all.OK() {
		t.Error("全 OK が OK() で false")
	}
	some := Result{Checks: []Check{{OK: true}, {OK: false}}}
	if some.OK() {
		t.Error("一部 NG が OK() で true")
	}
}

func TestIsExecutable(t *testing.T) {
	// /bin/sh は実行可能ファイルとして存在するはず。
	if !isExecutable("/bin/sh") {
		t.Error("/bin/sh が実行可能と判定されない")
	}
	if isExecutable("/bin") {
		t.Error("ディレクトリが実行可能と誤判定された")
	}
	if isExecutable("/no/such/file") {
		t.Error("存在しないパスが実行可能と誤判定された")
	}
}

func TestDeviceNr(t *testing.T) {
	if got := deviceNr("/dev/video10"); got != "10" {
		t.Errorf("deviceNr=/dev/video10 -> %q", got)
	}
	if got := deviceNr("weird"); got != "10" {
		t.Errorf("deviceNr フォールバックが効かない: %q", got)
	}
	if got := deviceNr("/dev/videoX"); got != "10" {
		t.Errorf("非数字サフィックスでフォールバックしない: %q", got)
	}
	if got := deviceNr("/dev/video0"); got != "0" {
		t.Errorf("deviceNr=/dev/video0 -> %q", got)
	}
}
