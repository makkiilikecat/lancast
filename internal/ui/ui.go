// Package ui は Gio を用いた GUI を構築する。コアロジックは持たず、
// config/ffmpeg/deps/runner を束ねて描画と操作だけを担う薄い層。
package ui

import (
	"fmt"
	"image"
	"image/color"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"lancast/internal/config"
	"lancast/internal/deps"
	"lancast/internal/ffmpeg"
	"lancast/internal/runner"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

type tab int

const (
	tabHost tab = iota
	tabClient
	tabSetup
)

// App は GUI の状態を保持する。
type App struct {
	th  *material.Theme
	win *app.Window
	cur tab

	tabHostBtn, tabClientBtn, tabSetupBtn widget.Clickable

	// Host 入力。
	hWidth, hHeight, hFPS, hBitrate, hPort widget.Editor
	hSource, hDestIP, hExtra               widget.Editor
	hCursor                                widget.Bool
	hEncBtn                                widget.Clickable
	hEncoders                              []string
	hEncIdx                                int
	presets                                []presetBtn
	hStart, hStop, hClear, hRecheck        widget.Clickable
	hPrev                                  widget.Editor
	hPrevCache                             string
	hLog                                   widget.List

	// Client 入力。
	cPort, cFifo, cDevice, cPixFmt, cExtra widget.Editor
	cLowDelay                              widget.Bool
	cStart, cStop, cClear, cRecheck        widget.Clickable
	cPrev                                  widget.Editor
	cPrevCache                             string
	cLog                                   widget.List

	setupRecheck           widget.Clickable
	hGotoSetup, cGotoSetup widget.Clickable
	hMacPerm               widget.Clickable
	hostFix                [6]fixField
	clientFix              [6]fixField

	hBackend string // OS 由来のキャプチャバックエンド（UI では編集しない）

	hostRunner, clientRunner *runner.Runner
	hostDeps, clientDeps     deps.Result
	ffmpegBin                string
	status                   string
}

type fixField struct {
	ed    widget.Editor
	cache string
}

// presetBtn は解像度プリセットボタン1個。
type presetBtn struct {
	label string
	w, h  string
	btn   widget.Clickable
}

// NewApp は保存済み設定を読み込んで App を初期化する。
func NewApp() *App {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))

	a := &App{
		th:           th,
		hostRunner:   runner.New(),
		clientRunner: runner.New(),
	}
	a.hLog.Axis = layout.Vertical
	a.hLog.ScrollToEnd = true
	a.cLog.Axis = layout.Vertical
	a.cLog.ScrollToEnd = true

	for _, e := range []*widget.Editor{&a.hWidth, &a.hHeight, &a.hFPS, &a.hBitrate, &a.hPort, &a.cPort, &a.cFifo} {
		e.SingleLine = true
		e.Filter = "0123456789"
	}
	for _, e := range []*widget.Editor{&a.hSource, &a.hDestIP, &a.hExtra, &a.cDevice, &a.cPixFmt, &a.cExtra} {
		e.SingleLine = true
	}
	a.presets = []presetBtn{
		{label: "720p", w: "1280", h: "720"},
		{label: "1080p", w: "1920", h: "1080"},
		{label: "1440p", w: "2560", h: "1440"},
	}
	a.hPrev.ReadOnly = true
	a.cPrev.ReadOnly = true
	for i := range a.hostFix {
		a.hostFix[i].ed.ReadOnly = true
		a.hostFix[i].ed.SingleLine = true
		a.clientFix[i].ed.ReadOnly = true
		a.clientFix[i].ed.SingleLine = true
	}

	cfg, _ := config.Load()
	a.loadFromConfig(cfg)
	a.refreshDeps()
	return a
}

func (a *App) loadFromConfig(cfg config.Config) {
	h := cfg.Host
	a.hEncoders = ffmpeg.EncodersForOS(runtime.GOOS)
	a.hEncIdx = encoderIndex(a.hEncoders, h.Encoder)
	a.hWidth.SetText(strconv.Itoa(h.Width))
	a.hHeight.SetText(strconv.Itoa(h.Height))
	a.hFPS.SetText(strconv.Itoa(h.FPS))
	a.hBitrate.SetText(strconv.Itoa(h.Bitrate))
	a.hPort.SetText(strconv.Itoa(h.DestPort))
	a.hSource.SetText(h.Source)
	a.hDestIP.SetText(h.DestIP)
	a.hExtra.SetText(h.ExtraArgs)
	a.hCursor.Value = h.CaptureCursor
	a.hBackend = h.Backend

	c := cfg.Client
	a.cPort.SetText(strconv.Itoa(c.ListenPort))
	a.cFifo.SetText(strconv.Itoa(c.FifoSize))
	a.cDevice.SetText(c.OutputDevice)
	a.cPixFmt.SetText(c.PixFmt)
	a.cExtra.SetText(c.ExtraArgs)
	a.cLowDelay.Value = c.LowDelay
}

func encoderIndex(list []string, enc string) int {
	if i := slices.Index(list, enc); i >= 0 {
		return i
	}
	return 0
}

func (a *App) assemble() config.Config {
	enc := ""
	if a.hEncIdx >= 0 && a.hEncIdx < len(a.hEncoders) {
		enc = a.hEncoders[a.hEncIdx]
	}
	return config.Config{
		Host: config.HostConfig{
			Backend:       a.hBackend,
			Source:        a.hSource.Text(),
			CaptureCursor: a.hCursor.Value,
			Width:         atoi(a.hWidth.Text()),
			Height:        atoi(a.hHeight.Text()),
			FPS:           atoi(a.hFPS.Text()),
			Bitrate:       atoi(a.hBitrate.Text()),
			Encoder:       enc,
			DestIP:        a.hDestIP.Text(),
			DestPort:      atoi(a.hPort.Text()),
			ExtraArgs:     a.hExtra.Text(),
		},
		Client: config.ClientConfig{
			ListenPort:   atoi(a.cPort.Text()),
			OutputDevice: a.cDevice.Text(),
			PixFmt:       a.cPixFmt.Text(),
			FifoSize:     atoi(a.cFifo.Text()),
			LowDelay:     a.cLowDelay.Value,
			ExtraArgs:    a.cExtra.Text(),
		},
	}
}

func (a *App) refreshDeps() {
	cfg := a.assemble()
	a.hostDeps = deps.CheckHost(cfg.Host)
	a.clientDeps = deps.CheckClient(cfg.Client)
	a.ffmpegBin, _ = deps.FFmpegPath()
	a.status = "依存: Host " + a.hostDeps.Summary() + " / Client " + a.clientDeps.Summary()
}

func (a *App) startHost() {
	cfg := a.assemble()
	if msg := cfg.Host.Validate(); msg != "" {
		a.status = "Host: " + msg
		return
	}
	_ = config.Save(cfg)
	a.refreshDeps()
	if !a.hostDeps.OK() {
		a.status = "Host: 依存が未充足です（Setup タブ参照）"
		return
	}
	if err := a.hostRunner.Start(a.ffmpegBin, ffmpeg.HostArgs(cfg.Host)); err != nil {
		a.status = "Host: " + err.Error()
	}
}

func (a *App) startClient() {
	cfg := a.assemble()
	if msg := cfg.Client.Validate(); msg != "" {
		a.status = "Client: " + msg
		return
	}
	_ = config.Save(cfg)
	a.refreshDeps()
	if !a.clientDeps.OK() {
		a.status = "Client: 依存が未充足です（Setup タブ参照）"
		return
	}
	// 起動前チェック: 受信ポートが既に使われていれば（前回の受信が残っている等）、
	// ffmpeg を起動しても bind に失敗して即死（exit 231）するだけなので、その前に弾く。
	if err := runner.UDPPortAvailable(cfg.Client.ListenPort); err != nil {
		a.status = fmt.Sprintf("Client: 受信ポート %d は使用中です。前回の受信がまだ動いていないか確認してください（`fuser -k %d/udp` で解放後に再度開始）。",
			cfg.Client.ListenPort, cfg.Client.ListenPort)
		return
	}
	if err := a.clientRunner.Start(a.ffmpegBin, ffmpeg.ClientArgs(cfg.Client)); err != nil {
		a.status = "Client: " + err.Error()
		return
	}
	a.status = "Client: 受信開始。この後 Discord を開き、カメラ「MacScreen」を選択してください。"
}

// Run はウィンドウのイベントループを回す。
func (a *App) Run(w *app.Window) error {
	a.win = w
	a.hostRunner.OnUpdate = w.Invalidate
	a.clientRunner.OnUpdate = w.Invalidate
	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			a.handleEvents(gtx)
			a.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

// Shutdown は稼働中の ffmpeg を全て停止し、終了を待つ。ウィンドウを閉じた時・
// SIGINT/SIGTERM 受信時に呼び、ffmpeg の孤児化（ポート/デバイスの掴みっぱなし）を防ぐ。
// OOM/SIGKILL のような捕捉不能な強制終了は runner 側の Pdeathsig が担保する。
func (a *App) Shutdown() {
	a.hostRunner.Stop()
	a.clientRunner.Stop()
	// Stop は SIGINT→2秒後 SIGKILL でエスカレートする。最大 4 秒待って確実に落とす。
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if !a.hostRunner.Running() && !a.clientRunner.Running() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (a *App) handleEvents(gtx C) {
	if a.tabHostBtn.Clicked(gtx) {
		a.cur = tabHost
	}
	if a.tabClientBtn.Clicked(gtx) {
		a.cur = tabClient
	}
	if a.tabSetupBtn.Clicked(gtx) {
		a.cur = tabSetup
	}

	for i := range a.presets {
		if a.presets[i].btn.Clicked(gtx) {
			a.hWidth.SetText(a.presets[i].w)
			a.hHeight.SetText(a.presets[i].h)
		}
	}
	if a.hEncBtn.Clicked(gtx) && len(a.hEncoders) > 0 {
		a.hEncIdx = (a.hEncIdx + 1) % len(a.hEncoders)
	}
	if a.hStart.Clicked(gtx) {
		a.startHost()
	}
	if a.hStop.Clicked(gtx) {
		a.hostRunner.Stop()
	}
	if a.hClear.Clicked(gtx) {
		a.hostRunner.Clear()
	}
	if a.cStart.Clicked(gtx) {
		a.startClient()
	}
	if a.cStop.Clicked(gtx) {
		a.clientRunner.Stop()
	}
	if a.cClear.Clicked(gtx) {
		a.clientRunner.Clear()
	}
	// 短絡評価で Clicked のイベント消費が漏れないよう個別に評価する。
	r1 := a.hRecheck.Clicked(gtx)
	r2 := a.cRecheck.Clicked(gtx)
	r3 := a.setupRecheck.Clicked(gtx)
	if r1 || r2 || r3 {
		a.refreshDeps()
	}
	if a.hGotoSetup.Clicked(gtx) || a.cGotoSetup.Clicked(gtx) {
		a.cur = tabSetup
	}
	if a.hMacPerm.Clicked(gtx) {
		openScreenRecordingSettings()
	}
}

func (a *App) layout(gtx C) D {
	return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(a.tabBar),
			layout.Rigid(spacer(8)),
			layout.Flexed(1, func(gtx C) D {
				switch a.cur {
				case tabClient:
					return a.clientTab(gtx)
				case tabSetup:
					return a.setupTab(gtx)
				default:
					return a.hostTab(gtx)
				}
			}),
			layout.Rigid(spacer(6)),
			layout.Rigid(func(gtx C) D {
				l := material.Body2(a.th, a.runState())
				l.Color = color.NRGBA{R: 0x55, G: 0xc0, B: 0x6a, A: 0xff}
				return l.Layout(gtx)
			}),
			layout.Rigid(func(gtx C) D {
				return material.Body2(a.th, a.status).Layout(gtx)
			}),
		)
	})
}

// runState は Host/Client の稼働状態を1行で表す。
func (a *App) runState() string {
	h := "停止"
	if a.hostRunner.Running() {
		h = "● 配信中"
	}
	c := "停止"
	if a.clientRunner.Running() {
		c = "● 受信中"
	}
	return "Host: " + h + "   Client: " + c
}

func (a *App) tabBar(gtx C) D {
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return a.tabButton(gtx, &a.tabHostBtn, "Host (送信)", a.cur == tabHost) }),
		layout.Rigid(spacerW(6)),
		layout.Rigid(func(gtx C) D {
			return a.tabButton(gtx, &a.tabClientBtn, "Client (受信)", a.cur == tabClient)
		}),
		layout.Rigid(spacerW(6)),
		layout.Rigid(func(gtx C) D { return a.tabButton(gtx, &a.tabSetupBtn, "依存 / Setup", a.cur == tabSetup) }),
	)
}

func (a *App) presetRow(gtx C) D {
	children := []layout.FlexChild{layout.Rigid(material.Body2(a.th, "解像度: ").Layout)}
	for i := range a.presets {
		p := &a.presets[i]
		children = append(children,
			layout.Rigid(func(gtx C) D { return material.Button(a.th, &p.btn, p.label).Layout(gtx) }),
			layout.Rigid(spacerW(4)),
		)
	}
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, children...)
}

func (a *App) tabButton(gtx C, btn *widget.Clickable, label string, active bool) D {
	b := material.Button(a.th, btn, label)
	if !active {
		b.Background = color.NRGBA{R: 0x55, G: 0x55, B: 0x60, A: 0xff}
	}
	return b.Layout(gtx)
}

func (a *App) hostTab(gtx C) D {
	ready := a.hostDeps.OK()
	running := a.hostRunner.Running()

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(a.warnIfNotReady(ready, &a.hGotoSetup)),
		layout.Rigid(a.macPermRow()),
		layout.Rigid(a.presetRow),
		layout.Rigid(spacer(4)),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(a.labelCell("幅 × 高さ")),
				layout.Flexed(1, a.boxedEditor(&a.hWidth)),
				layout.Rigid(material.Body2(a.th, " × ").Layout),
				layout.Flexed(1, a.boxedEditor(&a.hHeight)),
			)
		}),
		layout.Rigid(a.field("FPS", &a.hFPS)),
		layout.Rigid(a.field("ビットレート(kbps)", &a.hBitrate)),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(a.labelCell("エンコーダ")),
				layout.Rigid(func(gtx C) D {
					return material.Button(a.th, &a.hEncBtn, a.encoderLabel()).Layout(gtx)
				}),
			)
		}),
		layout.Rigid(a.field("キャプチャ入力", &a.hSource)),
		layout.Rigid(a.hint(captureHint())),
		layout.Rigid(func(gtx C) D {
			return material.CheckBox(a.th, &a.hCursor, "カーソルを含める").Layout(gtx)
		}),
		layout.Rigid(a.field("送信先IP", &a.hDestIP)),
		layout.Rigid(a.field("ポート", &a.hPort)),
		layout.Rigid(a.field("追加引数", &a.hExtra)),
		layout.Rigid(spacer(4)),
		layout.Rigid(material.Body2(a.th, "実行コマンド:").Layout),
		layout.Rigid(a.previewBox(&a.hPrev, &a.hPrevCache, ffmpeg.Preview("ffmpeg", ffmpeg.HostArgs(a.assemble().Host)))),
		layout.Rigid(spacer(4)),
		layout.Rigid(a.controlRow(&a.hStart, &a.hStop, &a.hClear, &a.hRecheck, ready, running)),
		layout.Rigid(spacer(4)),
		layout.Rigid(material.Body2(a.th, "ログ:").Layout),
		layout.Flexed(1, a.logBox(&a.hLog, a.hostRunner.Lines())),
	)
}

func (a *App) clientTab(gtx C) D {
	ready := a.clientDeps.OK()
	running := a.clientRunner.Running()

	clientBanner := a.warnIfNotReady(ready, &a.cGotoSetup)
	if runtime.GOOS != "linux" {
		// 受信は Linux 専用。Setup では直せないので警告ではなく説明を出す。
		clientBanner = a.infoBanner("ℹ この PC は送信(Host)専用です。受信は Ubuntu 機の LANCast で行ってください（v4l2loopback は Linux のみ）。")
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(clientBanner),
		layout.Rigid(a.field("受信ポート", &a.cPort)),
		layout.Rigid(a.field("バッファ(fifo_size)", &a.cFifo)),
		layout.Rigid(a.field("出力デバイス", &a.cDevice)),
		layout.Rigid(a.field("ピクセル形式", &a.cPixFmt)),
		layout.Rigid(func(gtx C) D {
			return material.CheckBox(a.th, &a.cLowDelay, "低遅延 (nobuffer + low_delay)").Layout(gtx)
		}),
		layout.Rigid(a.field("追加引数", &a.cExtra)),
		layout.Rigid(spacer(4)),
		layout.Rigid(material.Body2(a.th, "実行コマンド:").Layout),
		layout.Rigid(a.previewBox(&a.cPrev, &a.cPrevCache, ffmpeg.Preview("ffmpeg", ffmpeg.ClientArgs(a.assemble().Client)))),
		layout.Rigid(spacer(4)),
		layout.Rigid(a.controlRow(&a.cStart, &a.cStop, &a.cClear, &a.cRecheck, ready, running)),
		layout.Rigid(spacer(4)),
		layout.Rigid(material.Body2(a.th, "ログ:").Layout),
		layout.Flexed(1, a.logBox(&a.cLog, a.clientRunner.Lines())),
	)
}

func (a *App) setupTab(gtx C) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(material.Body2(a.th, "不足分は下のコマンドで解消してください（自動実行はしません）。").Layout),
		layout.Rigid(spacer(4)),
		layout.Rigid(func(gtx C) D { return material.Button(a.th, &a.setupRecheck, "再チェック").Layout(gtx) }),
		layout.Rigid(spacer(6)),
		layout.Rigid(material.H6(a.th, "Host (送信)").Layout),
		layout.Rigid(a.checksView(&a.hostDeps, a.hostFix[:])),
		layout.Rigid(spacer(8)),
		layout.Rigid(material.H6(a.th, "Client (受信)").Layout),
		layout.Rigid(a.checksView(&a.clientDeps, a.clientFix[:])),
	)
}

func (a *App) checksView(r *deps.Result, fix []fixField) func(C) D {
	return func(gtx C) D {
		var children []layout.FlexChild
		fixIdx := 0
		for i := range r.Checks {
			c := &r.Checks[i]
			line := statusIcon(c.OK) + " " + c.Name + " — " + c.Detail
			children = append(children, layout.Rigid(material.Body2(a.th, line).Layout))
			if !c.OK && c.Fix != "" && fixIdx < len(fix) {
				ff := &fix[fixIdx]
				fixIdx++
				setIfChanged(&ff.ed, &ff.cache, c.Fix)
				children = append(children, layout.Rigid(a.boxedEditor(&ff.ed)))
			}
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	}
}

// ---- 小さな部品 ----

// warnIfNotReady は依存未充足時に、クリックで Setup タブへ飛ぶ警告ボタンを描く。
func (a *App) warnIfNotReady(ready bool, btn *widget.Clickable) func(C) D {
	return func(gtx C) D {
		if ready {
			return D{}
		}
		b := material.Button(a.th, btn, "⚠ 依存が未充足です — タップして Setup を開く")
		b.Background = color.NRGBA{R: 0xa0, G: 0x55, B: 0x20, A: 0xff}
		return b.Layout(gtx)
	}
}

// infoBanner は青色の情報バナー（操作不能の理由説明など）を描く。
func (a *App) infoBanner(msg string) func(C) D {
	return func(gtx C) D {
		l := material.Body2(a.th, msg)
		l.Color = color.NRGBA{R: 0x6a, G: 0x9a, B: 0xff, A: 0xff}
		return l.Layout(gtx)
	}
}

// macPermRow は macOS でのみ「画面収録の許可」設定を開くボタンを描く。
func (a *App) macPermRow() func(C) D {
	return func(gtx C) D {
		if runtime.GOOS != "darwin" {
			return D{}
		}
		return material.Button(a.th, &a.hMacPerm, "画面収録を許可（システム設定を開く）").Layout(gtx)
	}
}

// hint は薄色の補足テキストを描く（空文字なら何も描かない）。
func (a *App) hint(s string) func(C) D {
	return func(gtx C) D {
		if s == "" {
			return D{}
		}
		l := material.Body2(a.th, s)
		l.Color = color.NRGBA{R: 0x99, G: 0x99, B: 0x99, A: 0xff}
		return l.Layout(gtx)
	}
}

// captureHint は OS 別のキャプチャ入力の補足を返す。
func captureHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "デバイス番号は『ffmpeg -f avfoundation -list_devices true -i \"\"』で確認。初回は システム設定>プライバシー>画面収録 の許可が必要。"
	case "linux":
		return "X11 は :0.0 など。ディスプレイ番号は環境変数 $DISPLAY を参照。"
	default:
		return ""
	}
}

func (a *App) field(label string, ed *widget.Editor) func(C) D {
	return func(gtx C) D {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(a.labelCell(label)),
			layout.Flexed(1, a.boxedEditor(ed)),
		)
	}
}

func (a *App) labelCell(label string) func(C) D {
	return func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Dp(unit.Dp(150))
		return material.Body2(a.th, label).Layout(gtx)
	}
}

func (a *App) boxedEditor(ed *widget.Editor) func(C) D {
	return func(gtx C) D {
		return widget.Border{
			Color:        color.NRGBA{R: 0x99, G: 0x99, B: 0x99, A: 0xff},
			Width:        unit.Dp(1),
			CornerRadius: unit.Dp(3),
		}.Layout(gtx, func(gtx C) D {
			return layout.UniformInset(unit.Dp(4)).Layout(gtx, func(gtx C) D {
				return material.Editor(a.th, ed, "").Layout(gtx)
			})
		})
	}
}

func (a *App) previewBox(ed *widget.Editor, cache *string, content string) func(C) D {
	return func(gtx C) D {
		setIfChanged(ed, cache, content)
		return a.boxedEditor(ed)(gtx)
	}
}

func (a *App) controlRow(start, stop, clear, recheck *widget.Clickable, ready, running bool) func(C) D {
	return func(gtx C) D {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(a.gatedButton(start, "▶ 開始", ready && !running)),
			layout.Rigid(spacerW(6)),
			layout.Rigid(a.gatedButton(stop, "■ 停止", running)),
			layout.Rigid(spacerW(6)),
			layout.Rigid(func(gtx C) D { return material.Button(a.th, clear, "ログ消去").Layout(gtx) }),
			layout.Rigid(spacerW(6)),
			layout.Rigid(func(gtx C) D { return material.Button(a.th, recheck, "再チェック").Layout(gtx) }),
		)
	}
}

// gatedButton は enabled=false のとき操作不能かつ淡色で表示する。
func (a *App) gatedButton(btn *widget.Clickable, label string, enabled bool) func(C) D {
	return func(gtx C) D {
		b := material.Button(a.th, btn, label)
		if !enabled {
			gtx = gtx.Disabled()
			b.Background = color.NRGBA{R: 0x44, G: 0x44, B: 0x48, A: 0xff}
		}
		return b.Layout(gtx)
	}
}

func (a *App) logBox(list *widget.List, lines []string) func(C) D {
	return func(gtx C) D {
		return widget.Border{
			Color:        color.NRGBA{R: 0x77, G: 0x77, B: 0x77, A: 0xff},
			Width:        unit.Dp(1),
			CornerRadius: unit.Dp(3),
		}.Layout(gtx, func(gtx C) D {
			return layout.UniformInset(unit.Dp(4)).Layout(gtx, func(gtx C) D {
				return material.List(a.th, list).Layout(gtx, len(lines), func(gtx C, i int) D {
					return material.Body2(a.th, lines[i]).Layout(gtx)
				})
			})
		})
	}
}

func (a *App) encoderLabel() string {
	if a.hEncIdx >= 0 && a.hEncIdx < len(a.hEncoders) {
		return fmt.Sprintf("(%d/%d) %s ▸", a.hEncIdx+1, len(a.hEncoders), a.hEncoders[a.hEncIdx])
	}
	return "(なし)"
}

func setIfChanged(ed *widget.Editor, cache *string, val string) {
	if *cache != val {
		ed.SetText(val)
		*cache = val
	}
}

func statusIcon(ok bool) string {
	if ok {
		return "[OK]"
	}
	return "[NG]"
}

func spacer(dp float32) func(C) D {
	return func(gtx C) D { return D{Size: image.Pt(0, gtx.Dp(unit.Dp(dp)))} }
}

func spacerW(dp float32) func(C) D {
	return func(gtx C) D { return D{Size: image.Pt(gtx.Dp(unit.Dp(dp)), 0)} }
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
