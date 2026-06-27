// lancast は LAN 内で画面を送受信し、受信側を仮想カメラ(v4l2loopback)へ流す
// Gio 製の単一バイナリ GUI アプリ。Host(送信)/Client(受信) を同一インスタンスで扱える。
//
// 引数なしで GUI 起動。-host / -client でヘッドレス（GUI なし）即時起動。
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"gioui.org/app"
	"gioui.org/unit"

	"lancast/internal/cli"
	"lancast/internal/config"
	"lancast/internal/ui"
)

func main() {
	var (
		host    = flag.Bool("host", false, "ヘッドレスで Host(送信)を即時起動")
		client  = flag.Bool("client", false, "ヘッドレスで Client(受信)を即時起動")
		debug   = flag.Bool("debug", false, "詳細ログを標準出力へ")
		dest    = flag.String("dest", "", "送信先IP (host)")
		port    = flag.Int("port", 0, "ポート (host=送信先 / client=受信)")
		device  = flag.String("device", "", "出力デバイス (client) 例 /dev/video10")
		source  = flag.String("source", "", "キャプチャ入力 (host) 例 3:none, :0.0")
		encoder = flag.String("encoder", "", "エンコーダ (host) 例 hevc_videotoolbox")
		bitrate = flag.Int("bitrate", 0, "ビットレート kbps (host)")
		fps     = flag.Int("fps", 0, "FPS (host)")
		size    = flag.String("size", "", "解像度 WxH (host) 例 1280x720")
		fifo    = flag.Int("fifo", 0, "fifo_size (client)")
		extra   = flag.String("extra", "", "ffmpeg 追加引数")
	)
	flag.Parse()

	if *host && *client {
		fmt.Fprintln(os.Stderr, "-host と -client は同時に指定できません")
		os.Exit(2)
	}

	if *host || *client {
		cfg, _ := config.Load()
		applyOverrides(&cfg, overrides{
			dest: *dest, port: *port, device: *device, source: *source,
			encoder: *encoder, bitrate: *bitrate, fps: *fps, size: *size,
			fifo: *fifo, extra: *extra,
		})
		mode := cli.ModeHost
		if *client {
			mode = cli.ModeClient
		}
		os.Exit(cli.RunHeadless(cfg, mode, *debug))
	}

	// GUI モード。
	go func() {
		a := ui.NewApp()
		w := new(app.Window)
		w.Option(app.Title("LANCast"), app.Size(unit.Dp(740), unit.Dp(860)))
		if err := a.Run(w); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

type overrides struct {
	dest, device, source, encoder, size, extra string
	port, bitrate, fps, fifo                   int
}

// applyOverrides は指定された（非ゼロ・非空の）フラグだけを設定へ反映する。
func applyOverrides(cfg *config.Config, o overrides) {
	if o.dest != "" {
		cfg.Host.DestIP = o.dest
	}
	if o.source != "" {
		cfg.Host.Source = o.source
	}
	if o.encoder != "" {
		cfg.Host.Encoder = o.encoder
	}
	if o.bitrate > 0 {
		cfg.Host.Bitrate = o.bitrate
	}
	if o.fps > 0 {
		cfg.Host.FPS = o.fps
	}
	if w, h, ok := parseSize(o.size); ok {
		cfg.Host.Width, cfg.Host.Height = w, h
	}
	if o.device != "" {
		cfg.Client.OutputDevice = o.device
	}
	if o.fifo > 0 {
		cfg.Client.FifoSize = o.fifo
	}
	if o.port > 0 {
		cfg.Host.DestPort = o.port
		cfg.Client.ListenPort = o.port
	}
	if o.extra != "" {
		cfg.Host.ExtraArgs = o.extra
		cfg.Client.ExtraArgs = o.extra
	}
}

// parseSize は "1280x720" を (1280, 720, true) に分解する。
func parseSize(s string) (int, int, bool) {
	if s == "" {
		return 0, 0, false
	}
	parts := strings.SplitN(strings.ToLower(s), "x", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	w, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}
