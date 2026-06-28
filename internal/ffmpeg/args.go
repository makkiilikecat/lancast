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

// hostInput はキャプチャバックエンド別の入力部分（-f … -i …）を args に付ける。
// HostArgs と HostPreviewArgs で共有する。
func hostInput(args []string, c config.HostConfig) []string {
	switch c.Backend {
	case "avfoundation":
		// 注: mac の avfoundation スクリーンキャプチャに -framerate を付けると
		// "Configuration of video device failed" を誘発するため付けない。
		args = append(args, "-f", "avfoundation")
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

	// 入力（キャプチャバックエンド別）。
	args = hostInput(args, c)

	// 映像フィルタ（解像度・FPS 正規化）。DAR が指定され、かつ Width:Height と
	// 異なる場合はアナモルフィック（圧縮映像＋表示比メタデータ）として送る。
	// setdar が H.264/HEVC の SAR に反映され、受信側が実比率を復元できる。
	vf := fmt.Sprintf("scale=%d:%d,fps=%d", c.Width, c.Height, c.FPS)
	if dar := darFilter(c); dar != "" {
		vf += "," + dar
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

// darFilter は送出フレームに付ける表示比メタデータ（setdar）を返す。
// DAR 未指定、または DAR が Width:Height と一致する（＝歪み無し）なら空。
func darFilter(c config.HostConfig) string {
	if c.DARNum <= 0 || c.DARDen <= 0 {
		return ""
	}
	if c.DARNum*c.Height == c.DARDen*c.Width {
		return "" // 既に正方ピクセル相当。メタデータ不要。
	}
	return fmt.Sprintf("setdar=%d/%d", c.DARNum, c.DARDen)
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

// ClientArgs は受信側 ffmpeg の引数列を生成する（v4l2 仮想カメラへ書き込み）。
func ClientArgs(c config.ClientConfig) []string {
	args := []string{"-hide_banner", "-loglevel", "warning", "-stats"}
	if c.LowDelay {
		args = append(args, "-fflags", "nobuffer", "-flags", "low_delay")
	}
	args = append(args, "-probesize", "500000", "-analyzeduration", "0")
	url := fmt.Sprintf("udp://@:%d?fifo_size=%d&overrun_nonfatal=1", c.ListenPort, c.FifoSize)
	args = append(args, "-i", url)

	extra := splitArgs(c.ExtraArgs)
	args = append(args, extra...)

	// ユーザーが追加引数で独自の映像フィルタを指定している場合、本機能の -vf と
	// 二重指定になり ffmpeg は片方を黙って捨てる。衝突を避けるためユーザー指定を
	// 優先し、本機能のフィルタは付けない（その旨は UI のヒントで周知する）。
	if vf := clientVF(c); vf != "" && !hasVideoFilter(extra) {
		args = append(args, "-vf", vf)
	}

	// 仮想カメラへ N fps の CFR で提示する。-r は出力レートを固定し、フレーム
	// 到達タイミングを安定させる（不定 framerate による Discord クラッシュ回避）。
	// 0 は無指定＝送信ストリーム任せ（従来挙動）。
	if c.FPS > 0 {
		args = append(args, "-r", strconv.Itoa(c.FPS))
	}

	args = append(args, "-pix_fmt", c.PixFmt, "-f", "v4l2", c.OutputDevice)
	return args
}

// HasVideoFilter は追加引数文字列に映像フィルタ指定が含まれるかを返す（UI 警告用）。
func HasVideoFilter(extraArgs string) bool {
	return hasVideoFilter(splitArgs(extraArgs))
}

// hasVideoFilter は引数列に映像フィルタ指定（-vf / -filter:v / -filter_complex）が
// 含まれるかを返す。
func hasVideoFilter(args []string) bool {
	for _, a := range args {
		if a == "-vf" || a == "-filter:v" || a == "-filter_complex" {
			return true
		}
	}
	return false
}

// clientVF は復元（SAR→正方ピクセル）と目標比率パディングのフィルタ鎖を組む。
// どちらも不要なら空（無加工＝従来挙動）。
func clientVF(c config.ClientConfig) string {
	var f []string
	if c.FPS > 0 {
		// フレームを N fps へ正規化（重複/間引き）し、CFR で仮想カメラへ渡す。
		// -r だけでなくフィルタ側でも整えることで、可変フレームレート入力でも
		// 仮想カメラへの到達タイミングが一定になり、消費側の負荷予測が安定する。
		f = append(f, "fps="+strconv.Itoa(c.FPS))
	}
	if c.RestoreAspect {
		// 受信 SAR を反映して実比率の幅へ伸長し、以後は正方ピクセルとして扱う。
		f = append(f, "scale='trunc(iw*sar/2)*2':ih", "setsar=1")
	}
	if num, den, ok := parseAspect(c.TargetAspect); ok {
		// 指定比率の枠へ収まるよう端を黒で埋める（切り取らない）。
		// 幅は 16 の倍数・高さは偶数へ切り上げ、v4l2 のシアーも同時に回避。
		f = append(f, padToAspect(num, den))
	} else if c.RestoreAspect {
		// 目標比率を使わない場合でも、復元後の幅を 16 整列してシアーを防ぐ。
		f = append(f, "pad='ceil(iw/16)*16':'ceil(ih/2)*2':0:0:color=black")
	}
	return strings.Join(f, ",")
}

// padToAspect は num:den 比へ「収める」pad フィルタを返す（中央寄せ・黒帯）。
func padToAspect(num, den int) string {
	return fmt.Sprintf(
		"pad=w='ceil(max(iw,ih*%[1]d/%[2]d)/16)*16':h='ceil(max(ih,iw*%[2]d/%[1]d)/2)*2':x='(ow-iw)/2':y='(oh-ih)/2':color=black",
		num, den)
}

// parseAspect は "16:9" のような比率を num,den へ分解する。"" は ok=false。
func parseAspect(s string) (num, den int, ok bool) {
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
	args = hostInput(args, c)
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
