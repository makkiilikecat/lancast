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

// splitArgs はユーザー追加引数を空白で分割する（簡易 shlex）。
func splitArgs(s string) []string {
	return strings.Fields(s)
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

// HostArgs は送信側 ffmpeg の引数列を生成する。
func HostArgs(c config.HostConfig) []string {
	args := []string{"-hide_banner", "-loglevel", "warning", "-stats"}

	// 入力（キャプチャバックエンド別）。
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

	// 映像フィルタ（解像度・FPS 正規化）。
	args = append(args, "-vf", fmt.Sprintf("scale=%d:%d,fps=%d", c.Width, c.Height, c.FPS))

	// コーデック。
	args = append(args, "-c:v", c.Encoder, "-b:v", fmt.Sprintf("%dk", c.Bitrate))
	args = append(args, encoderExtra(c.Encoder)...)
	args = append(args, "-an") // 音声なし

	// ユーザー追加引数（出力指定の直前 = 出力オプションとして効く）。
	args = append(args, splitArgs(c.ExtraArgs)...)

	// 出力（MPEG-TS over UDP）。pkt_size=1316 は TS パケット7個ぶんで、
	// 巨大データグラムによる断片化・欠落を避ける定石値。
	args = append(args, "-f", "mpegts", fmt.Sprintf("udp://%s:%d?pkt_size=1316", c.DestIP, c.DestPort))
	return args
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

	args = append(args, splitArgs(c.ExtraArgs)...)

	args = append(args, "-pix_fmt", c.PixFmt, "-f", "v4l2", c.OutputDevice)
	return args
}

// Preview は引数列を人間可読な単一行コマンドへ整形する（UI 表示用）。
func Preview(bin string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, bin)
	for _, a := range args {
		if strings.ContainsAny(a, " ?&") {
			parts = append(parts, "\""+a+"\"")
		} else {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " ")
}

func boolArg(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
