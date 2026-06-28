// Package ffmpeg は設定から ffmpeg コマンドライン引数を生成する純関数群を提供する。
package ffmpeg

import (
	"fmt"
	"strconv"
	"strings"

	"lancast/internal/config"
)

// EncodersForOS は当該 OS で host モードに提示するエンコーダ候補を返す。
// 実際に利用可能かは deps パッケージで別途検証する。
func EncodersForOS(goos string) []string {
	switch goos {
	case "darwin":
		return []string{"hevc_videotoolbox", "h264_videotoolbox", "libx264", "libx265"}
	case "linux":
		return []string{"libx264", "libx265", "h264_nvenc", "hevc_nvenc", "h264_vaapi"}
	case "windows":
		return []string{"h264_nvenc", "hevc_nvenc", "libx264", "libx265"}
	default:
		return []string{"libx264", "libx265"}
	}
}

// splitArgs はユーザー追加引数を空白で分割する。引用符（' と "）で囲んだ値は
// 1トークンとして扱い、内部の空白を保持する（簡易 shlex）。
func splitArgs(s string) []string {
	var args []string
	var cur strings.Builder
	inTok := false
	var quote rune // 0 / '\'' / '"'
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inTok = true
		case r == ' ' || r == '\t' || r == '\n':
			if inTok {
				args = append(args, cur.String())
				cur.Reset()
				inTok = false
			}
		default:
			cur.WriteRune(r)
			inTok = true
		}
	}
	if inTok {
		args = append(args, cur.String())
	}
	return args
}

// udpHost は IPv6 アドレスを URL 用に角括弧で囲む。
func udpHost(ip string) string {
	if strings.Contains(ip, ":") {
		return "[" + ip + "]"
	}
	return ip
}

// encoderExtra はエンコーダ固有の低遅延向け既定引数を返す。
// ユーザーの追加引数で上書き可能。
func encoderExtra(enc string) []string {
	switch enc {
	case "hevc_videotoolbox":
		return []string{"-realtime", "1", "-tag:v", "hvc1"}
	case "h264_videotoolbox":
		return []string{"-realtime", "1"}
	case "libx264", "libx265":
		return []string{"-preset", "ultrafast", "-tune", "zerolatency"}
	case "h264_nvenc", "hevc_nvenc":
		return []string{"-preset", "p1", "-tune", "ll"}
	default:
		return nil
	}
}

// usesVideotoolbox はエンコーダが Apple VideoToolbox（HW アクセラレータ）かを返す。
func usesVideotoolbox(enc string) bool {
	return enc == "hevc_videotoolbox" || enc == "h264_videotoolbox"
}

// hostInput はキャプチャバックエンド別の入力部分（-f … -i …）を args に付ける。
// HostArgs と HostPreviewArgs で共有する。avfPixFmt が非空のときは avfoundation 入力に
// -pixel_format を指定し、キャプチャ側へ直接その形式を出させる（uyvy422→nv12 等の
// CPU 変換を省くため）。
func hostInput(args []string, c config.HostConfig, avfPixFmt string) []string {
	switch c.Backend {
	case "avfoundation":
		// 注: mac の avfoundation スクリーンキャプチャに -framerate を付けると
		// "Configuration of video device failed" を誘発するため付けない。
		args = append(args, "-f", "avfoundation")
		if avfPixFmt != "" {
			args = append(args, "-pixel_format", avfPixFmt)
		}
		if c.CaptureCursor {
			args = append(args, "-capture_cursor", "1")
		}
		args = append(args, "-i", c.Source)
	case "x11grab":
		args = append(args, "-f", "x11grab", "-framerate", strconv.Itoa(c.FPS))
		args = append(args, "-draw_mouse", boolArg(c.CaptureCursor))
		args = append(args, "-i", c.Source)
	case "gdigrab":
		args = append(args, "-f", "gdigrab", "-framerate", strconv.Itoa(c.FPS))
		args = append(args, "-draw_mouse", boolArg(c.CaptureCursor))
		args = append(args, "-i", c.Source)
	default:
		args = append(args, "-f", c.Backend, "-i", c.Source)
	}
	return args
}

// HostArgs は送信側 ffmpeg の引数列を生成する。
func HostArgs(c config.HostConfig) []string {
	args := []string{"-hide_banner", "-loglevel", "warning", "-stats"}

	// avfoundation×VideoToolbox では、スケール/形式変換を GPU へ逃がす。
	// 既定の CPU 経路（swscale で scale+pixfmt 変換）が Retina フル解像度×高 FPS で
	// CPU を 100% 超まで食うため、avfoundation に nv12 を直接出させて uyvy422→nv12 の
	// CPU 変換を排除し、hwupload→scale_vt で GPU 上でスケールする。
	gpu := c.Backend == "avfoundation" && usesVideotoolbox(c.Encoder)

	// 映像フィルタ（解像度・FPS 正規化のみ）。送出は Width:Height をそのまま使う
	// WYSIWYG。アスペクトはユーザーが Width×Height で決め、受信側は無加工で表示する。
	var vf string
	if gpu {
		args = append(args, "-init_hw_device", "videotoolbox")
		args = hostInput(args, c, "nv12")
		// fps を hwupload の前に置き、間引くフレームをアップロードしない。
		vf = fmt.Sprintf("fps=%d,hwupload,scale_vt=%d:%d", c.FPS, c.Width, c.Height)
	} else {
		args = hostInput(args, c, "")
		vf = fmt.Sprintf("scale=%d:%d,fps=%d", c.Width, c.Height, c.FPS)
	}
	args = append(args, "-vf", vf)

	// コーデック。
	args = append(args, "-c:v", c.Encoder, "-b:v", fmt.Sprintf("%dk", c.Bitrate))
	args = append(args, encoderExtra(c.Encoder)...)
	args = append(args, "-an") // 音声なし

	// ユーザー追加引数（出力指定の直前 = 出力オプションとして効く）。
	args = append(args, splitArgs(c.ExtraArgs)...)

	// 出力（MPEG-TS over UDP）。pkt_size=1316 は TS パケット7個ぶんで、
	// 巨大データグラムによる断片化・欠落を避ける定石値。
	args = append(args, "-f", "mpegts", fmt.Sprintf("udp://%s:%d?pkt_size=1316", udpHost(c.DestIP), c.DestPort))
	return args
}

// Align16 は v4l2 のストライド・パディング由来のシアー（斜めズレ）を避けるため、
// 値を 16 の倍数（最低 16）へ最近接で丸める。
func Align16(v int) int {
	if v < 16 {
		return 16
	}
	return (v + 8) / 16 * 16
}

// PresetWidth は縦解像度 height と画面比 aw:ah から、比率を保った横解像度を返す
// （16 の倍数へ丸め）。aw/ah が無効なら 16:9 とみなす。
func PresetWidth(height, aw, ah int) int {
	if aw <= 0 || ah <= 0 {
		aw, ah = 16, 9
	}
	return Align16(height * aw / ah)
}

// clientPixFmt は仮想カメラへ書き込むピクセル形式。H.264/HEVC のデコード結果は
// 実質 yuv420p なので、これを待機・ライブ双方で固定して v4l2 フォーマットを一定にする。
const clientPixFmt = "yuv420p"

// placeholderFPS は待機映像の提示フレームレート（実時間ペース）。
const placeholderFPS = 30

// ClientArgs は受信側 ffmpeg の引数列を生成する。
// 受信ストリームを無加工で v4l2 仮想カメラへ流す（スケール・FPS 正規化・アスペクト処理は
// 一切しない）。解像度・FPS・アスペクトはホスト送出のまま。フォーマット確定はホストの責務。
func ClientArgs(c config.ClientConfig) []string {
	args := []string{"-hide_banner", "-loglevel", "warning", "-stats"}
	if c.LowDelay {
		args = append(args, "-fflags", "nobuffer", "-flags", "low_delay")
	}
	args = append(args, "-probesize", "500000", "-analyzeduration", "0")
	url := fmt.Sprintf("udp://@:%d?fifo_size=%d&overrun_nonfatal=1", c.ListenPort, c.FifoSize)
	args = append(args, "-i", url)
	args = append(args, splitArgs(c.ExtraArgs)...)
	args = append(args, "-pix_fmt", clientPixFmt, "-f", "v4l2", c.OutputDevice)
	return args
}

// PlaceholderColor は待機映像の背景色（ffmpeg color フィルタ用）。
const PlaceholderColor = "0x1e1e1e"

// ClientPlaceholderArgs は「待機中」映像を仮想カメラへ流し続ける引数列を生成する。
// 受信ストリームが無い間（開始直後・ホスト停止中）も仮想カメラを生かし続け、Discord が
// カメラを失わないようにする。w×h は直近に受信したホスト送出の解像度（supervisor が学習）で、
// ライブと同じ寸法・ピクセル形式にすることで待機⇄ライブのフォーマット食い違い（=斜めズレ）を防ぐ。
func ClientPlaceholderArgs(w, h int, c config.ClientConfig) []string {
	args := []string{"-hide_banner", "-loglevel", "warning", "-stats"}
	// -re で実時間ペースに制限する。これが無いと v4l2 出力は即時受理し、ffmpeg が
	// 待機フレームを全力生成して fps が数千まで暴走、待機中ずっと CPU を1コア焼き続ける。
	args = append(args, "-re", "-f", "lavfi", "-i",
		fmt.Sprintf("color=c=%s:s=%dx%d:r=%d", PlaceholderColor, w, h, placeholderFPS))
	// プレースホルダだと一目で分かるよう中央へテキストを重ねる。
	args = append(args, "-vf", placeholderVF(h))
	args = append(args, "-pix_fmt", clientPixFmt, "-f", "v4l2", c.OutputDevice)
	return args
}

// placeholderVF は待機映像に重ねる中央テキスト（2行）を drawtext で生成する。
// フォントは fontconfig 既定に任せる（Linux では Noto Sans CJK 等が選ばれ日本語も描画できる）。
// テキストには drawtext のパースを乱す : , ' \ % を含めない。
func placeholderVF(h int) string {
	title := h / 10
	sub := h / 22
	gap := h / 12
	return fmt.Sprintf(
		"drawtext=text='LANCast':fontcolor=white:fontsize=%d:x=(w-text_w)/2:y=(h-text_h)/2-%d,"+
			"drawtext=text='ホストの接続を待っています…':fontcolor=0x9aa0a6:fontsize=%d:x=(w-text_w)/2:y=(h-text_h)/2+%d",
		title, gap, sub, gap)
}

// ParseAspect は "16:9" のような比率を num,den へ分解する。"" は ok=false。
// プリセットの横ドット数算出（PresetWidth の基準比）に使う。
func ParseAspect(s string) (num, den int, ok bool) {
	i := strings.IndexByte(s, ':')
	if i <= 0 {
		return 0, 0, false
	}
	n, err1 := strconv.Atoi(s[:i])
	d, err2 := strconv.Atoi(s[i+1:])
	if err1 != nil || err2 != nil || n <= 0 || d <= 0 {
		return 0, 0, false
	}
	return n, d, true
}

// プレビュー用の既定値（低解像度・低フレームレートで負荷を抑える）。
const (
	previewWidth = 480
	previewFPS   = 10
)

// mjpegOutput は MJPEG を TCP へ吐く出力部分を args に付ける。
// scale=W:-2 はアスペクト比を保ったまま偶数高さに丸める。
func mjpegOutput(args []string, url string) []string {
	args = append(args, "-vf", fmt.Sprintf("scale=%d:-2,fps=%d", previewWidth, previewFPS))
	args = append(args, "-c:v", "mjpeg", "-q:v", "8", "-an", "-f", "mjpeg", url)
	return args
}

// HostPreviewArgs は送信側プレビュー用 ffmpeg の引数列を生成する。
// 本配信とは独立した別プロセスで画面をキャプチャし、MJPEG を url(TCP) へ送る。
// 本配信を巻き込まずにオン/オフできるよう、あえてプロセスを分離している。
func HostPreviewArgs(c config.HostConfig, url string) []string {
	args := []string{"-hide_banner", "-loglevel", "error"}
	args = hostInput(args, c, "")
	return mjpegOutput(args, url)
}

// ClientPreviewArgs は受信側プレビュー用 ffmpeg の引数列を生成する。
// 受信結果の v4l2 仮想カメラ（複数リーダ可）から読み出すため、本受信とは独立する。
func ClientPreviewArgs(c config.ClientConfig, url string) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-f", "v4l2", "-i", c.OutputDevice}
	return mjpegOutput(args, url)
}

// Preview は引数列を人間可読な単一行コマンドへ整形する（表示・コピー用）。
// シェルの特殊文字を含む引数は単一引用符で囲み、貼り付け時の誤展開を防ぐ。
func Preview(bin string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuote(bin))
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n?&|;<>()$`\\\"'*") {
		return s
	}
	// 単一引用符で囲み、内部の ' は '\'' でエスケープ。
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func boolArg(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
