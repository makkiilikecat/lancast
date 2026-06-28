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

func TestClientArgs_FPSSetsCFR(t *testing.T) {
	c := config.DefaultConfigFor("linux").Client
	c.FPS = 60
	got := argStr(ClientArgs(c))
	// CFR 化（-vf 内 fps）と出力レート固定（-r）の両方が付く。
	for _, want := range []string{"fps=60", "-r 60", "-f v4l2 /dev/video10"} {
		if !strings.Contains(got, want) {
			t.Errorf("期待する引数 %q が無い: %s", want, got)
		}
	}
	// fps フィルタは出力指定より前（フィルタ鎖は -vf 側）に置かれること。
	if strings.Index(got, "fps=60") > strings.Index(got, "-f v4l2") {
		t.Errorf("fps フィルタは出力指定より前にあるべき: %s", got)
	}
}

func TestClientArgs_FPSZeroNoRate(t *testing.T) {
	c := config.DefaultConfigFor("linux").Client
	c.FPS = 0
	c.RestoreAspect = false
	c.TargetAspect = ""
	got := argStr(ClientArgs(c))
	if strings.Contains(got, "-r ") || strings.Contains(got, "fps=") {
		t.Errorf("FPS=0(ソースのまま)では fps/-r を付けない: %s", got)
	}
}

func TestClientArgs_FPSWithUserVF_RateStillForced(t *testing.T) {
	c := config.DefaultConfigFor("linux").Client
	c.FPS = 60
	c.ExtraArgs = "-vf hflip"
	got := argStr(ClientArgs(c))
	// ユーザー -vf 優先で本機能の fps フィルタは付けないが、-r は独立して付く。
	if strings.Contains(got, "fps=60") {
		t.Errorf("ユーザー -vf がある場合は fps フィルタを付けない: %s", got)
	}
	if !strings.Contains(got, "-r 60") {
		t.Errorf("ユーザー -vf があっても出力 -r は固定する: %s", got)
	}
	if strings.Count(got, "-vf") != 1 {
		t.Errorf("-vf が二重指定になっている: %s", got)
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

func TestAlign16(t *testing.T) {
	cases := map[int]int{0: 16, 8: 16, 1000: 1008, 1280: 1280, 1366: 1360, 1152: 1152}
	for in, want := range cases {
		if got := Align16(in); got != want {
			t.Errorf("Align16(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestPresetWidth(t *testing.T) {
	// 16:10 の 720p → 1152（16 整列、比率維持）。
	if got := PresetWidth(720, 1920, 1200); got != 1152 {
		t.Errorf("PresetWidth(720,16:10) = %d, want 1152", got)
	}
	// 画面比不明なら 16:9 とみなす → 1280。
	if got := PresetWidth(720, 0, 0); got != 1280 {
		t.Errorf("PresetWidth(720,unknown) = %d, want 1280", got)
	}
}

func TestHostArgs_Anamorphic_SetsDAR(t *testing.T) {
	c := config.DefaultConfigFor("darwin").Host
	c.Width, c.Height = 1280, 720   // 16:9
	c.DARNum, c.DARDen = 1920, 1200 // 実画面 16:10
	got := argStr(HostArgs(c))
	if !strings.Contains(got, "setdar=1920/1200") {
		t.Errorf("アナモルフィック時に setdar が無い: %s", got)
	}
}

func TestHostArgs_NoAnamorphicWhenAspectMatches(t *testing.T) {
	c := config.DefaultConfigFor("darwin").Host
	c.Width, c.Height = 1280, 720
	c.DARNum, c.DARDen = 1920, 1080 // 16:9 = 出力比と一致
	if strings.Contains(argStr(HostArgs(c)), "setdar") {
		t.Errorf("比率一致時に setdar を付けてはならない")
	}
}

func TestClientArgs_RestoreAndPad(t *testing.T) {
	c := config.DefaultConfigFor("linux").Client
	c.RestoreAspect = true
	c.TargetAspect = "16:9"
	got := argStr(ClientArgs(c))
	for _, want := range []string{
		"-vf", "scale='trunc(iw*sar/2)*2':ih", "setsar=1",
		"pad=w='ceil(max(iw,ih*16/9)/16)*16'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("期待する引数 %q が無い: %s", want, got)
		}
	}
}

func TestClientArgs_UserVFTakesPrecedence(t *testing.T) {
	c := config.DefaultConfigFor("linux").Client
	c.RestoreAspect = true
	c.TargetAspect = "16:9"
	c.ExtraArgs = "-vf hflip"
	got := argStr(ClientArgs(c))
	// ユーザーの -vf を優先し、本機能の復元/pad フィルタは付けない（二重 -vf 回避）。
	if strings.Contains(got, "setsar=1") || strings.Contains(got, "pad=w=") {
		t.Errorf("追加引数に -vf がある場合、本機能のフィルタを付けてはならない: %s", got)
	}
	if strings.Count(got, "-vf") != 1 {
		t.Errorf("-vf が二重指定になっている: %s", got)
	}
}

func TestClientArgs_NoVFWhenDisabled(t *testing.T) {
	c := config.DefaultConfigFor("linux").Client
	c.RestoreAspect = false
	c.TargetAspect = ""
	c.FPS = 0 // fps 正規化も無効なら無加工
	if strings.Contains(argStr(ClientArgs(c)), "-vf") {
		t.Errorf("復元も目標比率も fps 正規化も無効なら -vf は付けない（従来挙動）")
	}
}

func TestHostPreviewArgs(t *testing.T) {
	c := config.DefaultConfigFor("darwin").Host
	got := argStr(HostPreviewArgs(c, "tcp://127.0.0.1:5555"))
	for _, want := range []string{
		"-f avfoundation", "-i 3:none",
		"scale=480:-2,fps=10", "-c:v mjpeg", "-f mjpeg tcp://127.0.0.1:5555",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("期待する引数 %q が無い: %s", want, got)
		}
	}
	// プレビューに本配信のエンコーダ/UDP 出力が混ざってはならない。
	if strings.Contains(got, "mpegts") || strings.Contains(got, "videotoolbox") {
		t.Errorf("プレビューに本配信用の出力が混入: %s", got)
	}
}

func TestClientPreviewArgs(t *testing.T) {
	c := config.DefaultConfigFor("linux").Client
	c.OutputDevice = "/dev/video10"
	got := argStr(ClientPreviewArgs(c, "tcp://127.0.0.1:5555"))
	for _, want := range []string{
		"-f v4l2 -i /dev/video10",
		"scale=480:-2,fps=10", "-c:v mjpeg", "-f mjpeg tcp://127.0.0.1:5555",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("期待する引数 %q が無い: %s", want, got)
		}
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
