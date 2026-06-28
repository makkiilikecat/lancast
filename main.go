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
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"gioui.org/app"
	"gioui.org/unit"

	"lancast/internal/cli"
	"lancast/internal/config"
	"lancast/internal/runner"
	"lancast/internal/ui"
)

const version = "0.1.0"

func main() {
	// 親監視 watchdog として re-exec された場合のディスパッチ（最優先）。
	// flag.Parse より前に処理し、専用引数で即 RunWatchdog へ入る（GUI/ヘッドレスは起動しない）。
	if len(os.Args) >= 4 && os.Args[1] == runner.WatchdogArg {
		ppid, _ := strconv.Atoi(os.Args[2])
		cpid, _ := strconv.Atoi(os.Args[3])
		runner.RunWatchdog(ppid, cpid)
		return
	}

	var (
		host    = flag.Bool("host", false, "ヘッドレスで Host(送信)を即時起動")
		client  = flag.Bool("client", false, "ヘッドレスで Client(受信)を即時起動")
		debug   = flag.Bool("debug", false, "詳細ログ（設定内容・依存チェック）を出力")
		showVer = flag.Bool("version", false, "バージョンを表示して終了")
		dest    = flag.String("dest", "", "送信先 `IP` (host)")
		port    = flag.Int("port", 0, "`ポート` (host=送信先 / client=受信)")
		device  = flag.String("device", "", "出力 `デバイス` (client) 例 /dev/video10")
		source  = flag.String("source", "", "キャプチャ `入力` (host) 例 3:none, :0.0")
		encoder = flag.String("encoder", "", "`エンコーダ` (host) 例 hevc_videotoolbox")
		bitrate = flag.Int("bitrate", 0, "ビットレート `kbps` (host)")
		fps     = flag.Int("fps", 0, "`FPS` (host=送出)")
		size    = flag.String("size", "", "解像度 `WxH` (host) 例 1280x720")
		fifo    = flag.Int("fifo", 0, "受信バッファ `fifo_size` (client)")
		extra   = flag.String("extra", "", "ffmpeg 追加 `引数`")
	)
	flag.Usage = usage
	flag.Parse()

	if *showVer {
		fmt.Println("lancast", version)
		return
	}

	if *host && *client {
		fmt.Fprintln(os.Stderr, "[lancast] -host と -client は同時に指定できません")
		os.Exit(2)
	}

	o := overrides{
		dest: *dest, port: *port, device: *device, source: *source,
		encoder: *encoder, bitrate: *bitrate, fps: *fps, size: *size,
		fifo: *fifo, extra: *extra,
	}

	// 上書きフラグだけ指定して -host/-client を付け忘れた場合の注意喚起。
	if !*host && !*client && o.any() {
		fmt.Fprintln(os.Stderr, "[lancast] 注意: 上書きフラグが指定されましたが -host / -client が無いため GUI で起動します（ヘッドレスには -host か -client が必要）")
	}

	if *host || *client {
		cfg, _ := config.Load()
		applyOverrides(&cfg, o)
		mode := cli.ModeHost
		if *client {
			mode = cli.ModeClient
		}
		os.Exit(cli.RunHeadless(cfg, mode, *debug))
	}

	// GUI モード。
	a := ui.NewApp()

	// SIGINT/SIGTERM を捕捉し、ffmpeg を停止してから終了する。
	// （OOM/SIGKILL のような捕捉不能な強制終了に対しては、Linux は runner の Pdeathsig、
	//  macOS は runner が起動する watchdog プロセスが ffmpeg を道連れにする安全網となる。）
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		a.Shutdown()
		os.Exit(0)
	}()

	go func() {
		w := new(app.Window)
		w.Option(app.Title("LANCast"), app.Size(unit.Dp(740), unit.Dp(860)))
		err := a.Run(w)
		a.Shutdown() // ウィンドウを閉じた時も ffmpeg を確実に停止する
		if err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

func usage() {
	d := config.DefaultConfig()
	fmt.Fprint(os.Stderr, `lancast `+version+` — LAN 画面キャスト (送信=host / 受信=client)

使い方:
  lancast                 GUI を起動
  lancast -host           送信(Host)をヘッドレス即時起動
  lancast -client         受信(Client)をヘッドレス即時起動

モード (排他):
  -host       送信(Host)を即時起動
  -client     受信(Client)を即時起動

共通:
  -debug      詳細ログ（設定内容・依存チェック）
  -extra ARG  ffmpeg 追加引数
  -version    バージョン表示

Host(送信) 上書き:
  -dest IP -port N -source SPEC -encoder NAME -bitrate KBPS -fps N -size WxH

Client(受信) 上書き:
  -port N -device PATH -fifo N（受信は無加工。解像度/FPS/アスペクトはホスト送出のまま）

省略時は保存済み設定、無ければ既定値を使用。
  現在の既定値(host):   size=`+fmt.Sprintf("%dx%d fps=%d bitrate=%dk encoder=%s", d.Host.Width, d.Host.Height, d.Host.FPS, d.Host.Bitrate, d.Host.Encoder)+`
  現在の既定値(client): port=`+fmt.Sprintf("%d device=%s", d.Client.ListenPort, d.Client.OutputDevice)+`
`)
}

type overrides struct {
	dest, device, source, encoder, size, extra string
	port, bitrate, fps, fifo                   int
}

// any は上書きフラグが1つでも指定されたかを返す。
func (o overrides) any() bool {
	return o.dest != "" || o.device != "" || o.source != "" || o.encoder != "" ||
		o.size != "" || o.extra != "" ||
		o.port != 0 || o.bitrate != 0 || o.fps != 0 || o.fifo != 0
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
