package ffmpeg

import (
	"strings"
	"testing"

	"lancast/internal/config"
)

// argStr はスライスを空白連結して部分一致検査を容易にする。
func argStr(a []string) string { return strings.Join(a, " ") }

func TestHostArgs_AVFoundation_NoFramerateAtInput(t *testing.T) {
	c := config.DefaultConfigFor("darwin").Host
	got := argStr(HostArgs(c))

	// avfoundation 入力に -framerate を付けてはならない（device config 失敗回避）。
	if strings.Contains(got, "-framerate") {
		t.Errorf("avfoundation 入力に -framerate が含まれている: %s", got)
	}
	for _, want := range []string{
		"-f avfoundation", "-capture_cursor 1", "-i 3:none",
		"scale=1280:720,fps=30", "-c:v hevc_videotoolbox", "-b:v 20000k",
		"-realtime 1", "-tag:v hvc1", "-an",
		"-f mpegts udp://192.168.0.215:5004?pkt_size=1316",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("期待する引数 %q が無い: %s", want, got)
		}
	}
}

func TestHostArgs_X11Grab_HasFramerateAtInput(t *testing.T) {
	c := config.DefaultConfigFor("linux").Host
	got := argStr(HostArgs(c))

	// x11grab はキャプチャ FPS を入力側 -framerate で指定する必要がある。
	if !strings.Contains(got, "-f x11grab -framerate 30") {
		t.Errorf("x11grab 入力に -framerate が無い: %s", got)
	}
	if !strings.Contains(got, "-draw_mouse 1") {
		t.Errorf("draw_mouse が無い: %s", got)
	}
	if !strings.Contains(got, "-preset ultrafast -tune zerolatency") {
		t.Errorf("libx264 既定の低遅延引数が無い: %s", got)
	}
}

func TestHostArgs_CaptureCursorOff(t *testing.T) {
	c := config.DefaultConfigFor("darwin").Host
	c.CaptureCursor = false
	got := argStr(HostArgs(c))
	if strings.Contains(got, "-capture_cursor 1") {
		t.Errorf("カーソル無効なのに capture_cursor が有効: %s", got)
	}

	cl := config.DefaultConfigFor("linux").Host
	cl.CaptureCursor = false
	gotl := argStr(HostArgs(cl))
	if !strings.Contains(gotl, "-draw_mouse 0") {
		t.Errorf("linux でカーソル無効が反映されていない: %s", gotl)
	}
}

func TestHostArgs_ExtraArgsBeforeOutput(t *testing.T) {
	c := config.DefaultConfigFor("darwin").Host
	c.ExtraArgs = "-g 60 -bf 0"
	args := HostArgs(c)
	got := argStr(args)
	if !strings.Contains(got, "-g 60 -bf 0") {
		t.Fatalf("追加引数が反映されていない: %s", got)
	}
	// 追加引数は出力指定(-f mpegts)より前に来ること。
	gi := indexOf(args, "-g")
	oi := indexOf(args, "mpegts")
	if gi < 0 || oi < 0 || gi > oi {
		t.Errorf("追加引数が出力指定より後ろにある: %s", got)
	}
}

func TestClientArgs(t *testing.T) {
	c := config.DefaultConfigFor("linux").Client
	got := argStr(ClientArgs(c))
	for _, want := range []string{
		"-fflags nobuffer", "-flags low_delay",
		"udp://@:5004?fifo_size=1000000&overrun_nonfatal=1",
		"-pix_fmt yuv420p", "-f v4l2 /dev/video10",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("期待する引数 %q が無い: %s", want, got)
		}
	}
}

func TestClientArgs_LowDelayOff(t *testing.T) {
	c := config.DefaultConfigFor("linux").Client
	c.LowDelay = false
	got := argStr(ClientArgs(c))
	if strings.Contains(got, "nobuffer") {
		t.Errorf("low_delay 無効なのに nobuffer がある: %s", got)
	}
}

func TestEncodersForOS(t *testing.T) {
	if EncodersForOS("darwin")[0] != "hevc_videotoolbox" {
		t.Error("darwin の既定エンコーダ先頭が hevc_videotoolbox でない")
	}
	if len(EncodersForOS("linux")) == 0 {
		t.Error("linux のエンコーダ候補が空")
	}
}

func TestPreview_QuotesSpecialChars(t *testing.T) {
	got := Preview("ffmpeg", []string{"-i", "udp://@:5004?fifo_size=1"})
	if !strings.Contains(got, "'udp://@:5004?fifo_size=1'") {
		t.Errorf("特殊文字を含む引数が引用されていない: %s", got)
	}
	// 通常の引数は引用しない。
	if !strings.Contains(got, "ffmpeg -i ") {
		t.Errorf("通常引数が不必要に引用されている: %s", got)
	}
}

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"-g 60 -bf 0", []string{"-g", "60", "-bf", "0"}},
		{`-x264-params "keyint=60:bframes=0"`, []string{"-x264-params", "keyint=60:bframes=0"}},
		{`-metadata title='foo bar'`, []string{"-metadata", "title=foo bar"}},
		{"  a   b  ", []string{"a", "b"}},
	}
	for _, c := range cases {
		got := splitArgs(c.in)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("splitArgs(%q)=%v want %v", c.in, got, c.want)
		}
	}
}

func TestHostArgs_IPv6Bracket(t *testing.T) {
	c := config.DefaultConfigFor("darwin").Host
	c.DestIP = "fe80::1"
	got := argStr(HostArgs(c))
	if !strings.Contains(got, "udp://[fe80::1]:5004") {
		t.Errorf("IPv6 が角括弧で囲まれていない: %s", got)
	}
}

func indexOf(a []string, s string) int {
	for i, v := range a {
		if v == s {
			return i
		}
	}
	return -1
}
