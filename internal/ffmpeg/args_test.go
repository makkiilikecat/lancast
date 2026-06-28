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
	// avfoundation×VideoToolbox では GPU 縮小経路（nv12 直取り + hwupload + scale_vt）。
	// 縮小はアスペクト比保持（decrease 相当の式）で行い、pad は hwdownload 後に CPU で行う。
	c := config.DefaultConfigFor("darwin").Host
	got := argStr(HostArgs(c))
	for _, want := range []string{
		"-init_hw_device videotoolbox",
		"-f avfoundation -pixel_format nv12",
		"-vf fps=30,hwupload,scale_vt=",
		"hwdownload,format=nv12,pad=1280:720:(ow-iw)/2:(oh-ih)/2:color=black",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("GPU 縮小経路に期待する引数 %q が無い: %s", want, got)
		}
	}
	// アスペクト比保持＋拡大抑止の式（min(1, min(w/iw, h/ih))）が入っていること。
	if !strings.Contains(got, `min(1\,min(1280/iw`) {
		t.Errorf("GPU 経路にアスペクト保持＋拡大抑止の縮小式が無い: %s", got)
	}
	// CPU の swscale 経路（scale=W:H...）は出てはならない。
	if strings.Contains(got, "scale=1280:720") {
		t.Errorf("VideoToolbox なのに CPU swscale 経路が混入: %s", got)
	}
}

func TestHostArgs_SoftwareEncoderKeepsCPUScale(t *testing.T) {
	// libx264 等のソフトエンコーダでは GPU 往復は無意味なため CPU の fit+pad 経路を使う。
	c := config.DefaultConfigFor("darwin").Host
	c.Encoder = "libx264"
	got := argStr(HostArgs(c))
	want := "-vf scale=1280:720:force_original_aspect_ratio=decrease:force_divisible_by=2," +
		"pad=1280:720:(ow-iw)/2:(oh-ih)/2:color=black,fps=30"
	if !strings.Contains(got, want) {
		t.Errorf("ソフトエンコーダで CPU fit+pad 経路が無い: %s", got)
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

func TestHostArgs_FitPadLetterbox(t *testing.T) {
	// 出力は常に Width×Height ちょうどで、引き伸ばし（scale=W:H 単独）は使わない。
	// CPU・GPU いずれも decrease で縮小し pad で枠へ収める（歪み無し）。
	cpu := config.DefaultConfigFor("linux").Host // libx264 = CPU 経路
	cpu.Width, cpu.Height = 1920, 1080
	got := argStr(HostArgs(cpu))
	if !strings.Contains(got, "force_original_aspect_ratio=decrease") {
		t.Errorf("CPU 経路で decrease 縮小が無い: %s", got)
	}
	if !strings.Contains(got, "pad=1920:1080:(ow-iw)/2:(oh-ih)/2:color=black") {
		t.Errorf("CPU 経路で pad 枠が 1920x1080 でない: %s", got)
	}
	// 引き伸ばし（アスペクト無視の scale=W:H,fps）は残っていないこと。
	if strings.Contains(got, "scale=1920:1080,fps") {
		t.Errorf("引き伸ばし経路が残存: %s", got)
	}

	gpu := config.DefaultConfigFor("darwin").Host // hevc_videotoolbox = GPU 経路
	gpu.Width, gpu.Height = 1920, 1080
	gg := argStr(HostArgs(gpu))
	if !strings.Contains(gg, "pad=1920:1080:(ow-iw)/2:(oh-ih)/2:color=black") {
		t.Errorf("GPU 経路で pad 枠が 1920x1080 でない: %s", gg)
	}
	if strings.Contains(gg, "scale_vt=1920:1080") {
		t.Errorf("GPU 経路に引き伸ばし scale_vt=W:H が残存: %s", gg)
	}
}

func TestHostArgs_NoBarsUsesZeroCopyGPU(t *testing.T) {
	// 画面比＝出力枠比なら黒帯不要 → pad を省いた純 GPU ゼロコピー経路（hwdownload なし）。
	c := config.DefaultConfigFor("darwin").Host // hevc_videotoolbox
	c.Width, c.Height = 1280, 720
	c.ScreenW, c.ScreenH = 2560, 1440 // 16:9 = 出力枠比と一致
	got := argStr(HostArgs(c))
	if !strings.Contains(got, "-vf fps=30,hwupload,scale_vt=1280:720") {
		t.Errorf("黒帯不要時に純 GPU 経路でない: %s", got)
	}
	for _, ng := range []string{"hwdownload", "pad=", "force_original_aspect_ratio"} {
		if strings.Contains(got, ng) {
			t.Errorf("黒帯不要なのに pad 経路 %q が混入: %s", ng, got)
		}
	}
}

func TestHostArgs_NoBarsUsesPlainCPUScale(t *testing.T) {
	c := config.DefaultConfigFor("linux").Host // libx264 = CPU 経路
	c.Width, c.Height = 1280, 720
	c.ScreenW, c.ScreenH = 1920, 1080 // 16:9 一致
	got := argStr(HostArgs(c))
	if !strings.Contains(got, "-vf scale=1280:720,fps=30") {
		t.Errorf("黒帯不要時に素の CPU スケールでない: %s", got)
	}
	if strings.Contains(got, "pad=") {
		t.Errorf("黒帯不要なのに pad が混入: %s", got)
	}
}

func TestHostArgs_MismatchedAspectKeepsPad(t *testing.T) {
	// 16:10 画面 → 16:9 枠（約 11% 差）は黒帯必須。許容誤差を超えるので fit+pad のまま。
	c := config.DefaultConfigFor("darwin").Host
	c.Width, c.Height = 1280, 720 // 16:9
	c.ScreenW, c.ScreenH = 1920, 1200 // 16:10
	got := argStr(HostArgs(c))
	if !strings.Contains(got, "hwdownload,format=nv12,pad=1280:720:(ow-iw)/2:(oh-ih)/2:color=black") {
		t.Errorf("比不一致なのに pad 経路でない: %s", got)
	}
	// CPU 側も同様。
	cl := config.DefaultConfigFor("linux").Host
	cl.Width, cl.Height = 1280, 720
	cl.ScreenW, cl.ScreenH = 1920, 1200
	if !strings.Contains(argStr(HostArgs(cl)), "force_original_aspect_ratio=decrease") {
		t.Errorf("CPU 側で比不一致なのに fit+pad でない")
	}
}

func TestNoBars_RoundingTolerance(t *testing.T) {
	// 16 整列の丸め誤差（~1%以内）は一致扱い、16:10 vs 16:9 は別物扱い。
	cases := []struct {
		sw, sh, w, h int
		want         bool
	}{
		{2560, 1440, 1280, 720, true},  // 完全一致 16:9
		{3024, 1964, 1664, 1080, true}, // notch Mac, 16整列丸め → 1%未満
		{1920, 1200, 1920, 1080, false}, // 16:10 → 16:9
		{1920, 1080, 1440, 1080, false}, // 16:9 → 4:3
		{0, 0, 1280, 720, false},        // 画面不明は安全側
	}
	for _, c := range cases {
		h := config.HostConfig{ScreenW: c.sw, ScreenH: c.sh, Width: c.w, Height: c.h}
		if got := noBars(h); got != c.want {
			t.Errorf("noBars(screen %dx%d, out %dx%d)=%v want %v", c.sw, c.sh, c.w, c.h, got, c.want)
		}
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
