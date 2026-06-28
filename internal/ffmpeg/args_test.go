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
		"-c:v hevc_videotoolbox", "-b:v 20000k",
		"-realtime 1", "-tag:v hvc1", "-an",
		"-f mpegts udp://192.168.0.215:5004?pkt_size=1316",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("期待する引数 %q が無い: %s", want, got)
		}
	}
}

func TestHostArgs_VideotoolboxUsesGPUScale(t *testing.T) {
	// avfoundation×VideoToolbox では GPU スケール経路（nv12 直取り + hwupload + scale_vt）。
	c := config.DefaultConfigFor("darwin").Host
	got := argStr(HostArgs(c))
	for _, want := range []string{
		"-init_hw_device videotoolbox",
		"-f avfoundation -pixel_format nv12",
		"-vf fps=30,hwupload,scale_vt=1280:720",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("GPU スケール経路に期待する引数 %q が無い: %s", want, got)
		}
	}
	// CPU の swscale 経路（scale=W:H,fps）は出てはならない。
	if strings.Contains(got, "scale=1280:720,fps=30") {
		t.Errorf("VideoToolbox なのに CPU swscale 経路が混入: %s", got)
	}
}

func TestHostArgs_SoftwareEncoderKeepsCPUScale(t *testing.T) {
	// libx264 等のソフトエンコーダでは GPU 往復は無意味なため従来の CPU スケールを維持する。
	c := config.DefaultConfigFor("darwin").Host
	c.Encoder = "libx264"
	got := argStr(HostArgs(c))
	if !strings.Contains(got, "-vf scale=1280:720,fps=30") {
		t.Errorf("ソフトエンコーダで CPU スケール経路が無い: %s", got)
	}
	for _, ng := range []string{"-init_hw_device", "hwupload", "scale_vt", "-pixel_format"} {
		if strings.Contains(got, ng) {
			t.Errorf("ソフトエンコーダに GPU 専用引数 %q が混入: %s", ng, got)
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

func TestClientArgs_NoScalingNoRate(t *testing.T) {
	// 受信は無加工: スケール/FPS 正規化/アスペクト処理は一切しない。
	c := config.DefaultConfigFor("linux").Client
	got := argStr(ClientArgs(c))
	for _, ng := range []string{"-vf", "-r ", "scale", "fps=", "pad=", "setsar", "setdar"} {
		if strings.Contains(got, ng) {
			t.Errorf("受信は無加工のはずが %q が混入: %s", ng, got)
		}
	}
	// 追加引数は素通しされる（入力の後・出力の前）。
	c.ExtraArgs = "-flags2 +export_mvs"
	if !strings.Contains(argStr(ClientArgs(c)), "-flags2 +export_mvs") {
		t.Errorf("追加引数が反映されていない")
	}
}

func TestClientPlaceholderArgs(t *testing.T) {
	c := config.DefaultConfigFor("linux").Client
	got := argStr(ClientPlaceholderArgs(1152, 720, c))
	for _, want := range []string{
		"-re", "-f lavfi", "color=c=0x1e1e1e:s=1152x720:r=30",
		"drawtext=text='LANCast'", "ホストの接続を待っています",
		"-pix_fmt yuv420p", "-f v4l2 /dev/video10",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("待機映像に期待する引数 %q が無い: %s", want, got)
		}
	}
	// 寸法は引数で渡したホスト学習値に一致する（待機⇄ライブのフォーマット一致＝シアー防止）。
	if !strings.Contains(got, "s=1152x720") {
		t.Errorf("待機映像が指定寸法 1152x720 になっていない: %s", got)
	}
	// -re は入力(lavfi)より前に置く（入力オプションとして効かせる）。
	if strings.Index(got, "-re") > strings.Index(got, "-f lavfi") {
		t.Errorf("-re は入力指定より前にあるべき: %s", got)
	}
	// 待機映像は受信(UDP)入力を持たない。
	if strings.Contains(got, "udp://") {
		t.Errorf("待機映像に UDP 入力が混入: %s", got)
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

func TestHostArgs_NoSetDAR(t *testing.T) {
	// アナモルフィックは廃止。送出は Width:Height のまま、setdar は付けない。
	c := config.DefaultConfigFor("darwin").Host
	c.Width, c.Height = 1280, 720
	if strings.Contains(argStr(HostArgs(c)), "setdar") {
		t.Errorf("アナモルフィック廃止後に setdar が付いている")
	}
	cl := config.DefaultConfigFor("linux").Host // libx264 (CPU 経路)
	if strings.Contains(argStr(HostArgs(cl)), "setdar") {
		t.Errorf("CPU 経路でも setdar は付けない")
	}
}

func TestParseAspect(t *testing.T) {
	cases := []struct {
		in       string
		num, den int
		ok       bool
	}{
		{"16:9", 16, 9, true},
		{"16:10", 16, 10, true},
		{"21:9", 21, 9, true},
		{"9:21", 9, 21, true},
		{"", 0, 0, false},
		{"abc", 0, 0, false},
		{"16:0", 0, 0, false},
	}
	for _, c := range cases {
		n, d, ok := ParseAspect(c.in)
		if ok != c.ok || (ok && (n != c.num || d != c.den)) {
			t.Errorf("ParseAspect(%q)=(%d,%d,%v) want (%d,%d,%v)", c.in, n, d, ok, c.num, c.den, c.ok)
		}
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
