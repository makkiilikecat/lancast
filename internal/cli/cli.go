// Package cli はヘッドレス（GUI なし）での Host/Client 実行を提供する。
// SSH 越しの自動化・統合テスト・常駐起動に使う。
package cli

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"lancast/internal/config"
	"lancast/internal/deps"
	"lancast/internal/display"
	"lancast/internal/ffmpeg"
	"lancast/internal/runner"
)

// Mode はヘッドレス実行のモード。
type Mode string

const (
	ModeHost   Mode = "host"
	ModeClient Mode = "client"
)

func logf(format string, a ...any) {
	fmt.Printf("[lancast %s] "+format+"\n", append([]any{time.Now().Format("15:04:05")}, a...)...)
}

// RunHeadless は指定モードを GUI なしで起動し、ffmpeg のログを標準出力へ流す。
// SIGINT/SIGTERM で停止する。ffmpeg が自走終了した場合も戻る。終了コードを返す。
func RunHeadless(cfg config.Config, mode Mode, debug bool) int {
	var (
		valid string
		res   deps.Result
		args  []string
	)
	switch mode {
	case ModeHost:
		// 黒帯不要時の純 GPU ゼロコピー経路判定のため、画面解像度を検出して渡す
		// （非 mac では取得不可＝0 で、安全側の fit+pad になる）。
		if w, h, ok := display.MainAspect(); ok {
			cfg.Host.ScreenW, cfg.Host.ScreenH = w, h
		}
		valid = cfg.Host.Validate()
		res = deps.CheckHost(cfg.Host)
		args = ffmpeg.HostArgs(cfg.Host)
	case ModeClient:
		valid = cfg.Client.Validate()
		res = deps.CheckClient(cfg.Client)
		args = ffmpeg.ClientArgs(cfg.Client)
	default:
		fmt.Fprintln(os.Stderr, "[lancast] 不明なモード:", mode)
		return 2
	}

	if valid != "" {
		fmt.Fprintln(os.Stderr, "[lancast] 設定エラー:", valid+settingHint(mode))
		return 2
	}

	if debug {
		switch mode {
		case ModeHost:
			logf("debug: host=%+v", cfg.Host)
		case ModeClient:
			logf("debug: client=%+v", cfg.Client)
		}
	}

	printChecks(res)
	if !res.OK() {
		fmt.Fprintln(os.Stderr, "[lancast] 依存が未充足のため起動を中止しました（上記コマンドで解消してください。自動実行はしません）")
		return 1
	}

	bin, _ := deps.FFmpegPath()
	logf("mode=%s", mode)
	logf("ffmpeg=%s", bin)
	logf("exec: %s", ffmpeg.Preview(bin, args))

	if mode == ModeClient {
		return runClient(bin, cfg.Client)
	}

	r := runner.New()
	// frame= 進捗は流れすぎるため 2 秒に 1 回に間引き、最初の 1 回で「稼働中」を明示する。
	// OnLine は stdout/stderr の 2 本の pump goroutine から並行に呼ばれ得るため mu で保護する。
	var (
		mu        sync.Mutex
		streamUp  bool
		lastFrame time.Time
	)
	r.OnLine = func(s string) {
		if strings.HasPrefix(s, "frame=") {
			mu.Lock()
			if !streamUp {
				streamUp = true
				logf("ストリーム稼働中（映像フレームを確認）")
			}
			now := time.Now()
			if now.Sub(lastFrame) < 2*time.Second {
				mu.Unlock()
				return
			}
			lastFrame = now
			mu.Unlock()
		}
		fmt.Println(s)
	}
	if err := r.Start(bin, args); err != nil {
		fmt.Fprintln(os.Stderr, "[lancast] 起動失敗:", err)
		return 1
	}
	logf("%s:%d へ送信開始（UDP のため受信側の有無は検知できません。受信側を先に起動してください）", cfg.Host.DestIP, cfg.Host.DestPort)
	logf("停止するには Ctrl-C。")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	for {
		select {
		case <-sig:
			logf("停止シグナル受信、停止中…")
			r.Stop()
			if waitStopped(r) {
				logf("終了")
				return 0
			}
			fmt.Fprintln(os.Stderr, "[lancast] プロセスが時間内に停止しませんでした")
			return 1
		case <-time.After(200 * time.Millisecond):
			if !r.Running() {
				fmt.Fprintln(os.Stderr, "[lancast] ffmpeg が終了しました（上記ログを確認してください）")
				return 1
			}
		}
	}
}

// runClient は受信側を ClientSupervisor で常駐起動する。待機⇄ライブを自動で切り替え、
// ホストの開始/停止/解像度変更/一時切断に追従して仮想カメラを途切れさせない。
// SIGINT/SIGTERM で停止して 0 を返す。
func runClient(bin string, c config.ClientConfig) int {
	sup := runner.NewClientSupervisor()
	// OnLine は内部 Runner の stdout/stderr 2 本の pump goroutine から並行に呼ばれ得る。
	var (
		mu        sync.Mutex
		lastFrame time.Time
	)
	sup.OnLine = func(s string) {
		if strings.HasPrefix(s, "frame=") {
			mu.Lock()
			skip := time.Since(lastFrame) < 2*time.Second
			if !skip {
				lastFrame = time.Now()
			}
			mu.Unlock()
			if skip {
				return
			}
		}
		fmt.Println(s)
	}
	sup.OnState = func(st string) { logf("状態: %s", st) }

	live := ffmpeg.ClientArgs(c)
	phFn := func(w, h int) []string { return ffmpeg.ClientPlaceholderArgs(w, h, c) }
	// 受信解像度を学習したら設定へ永続化（次回の待機映像を最初からホスト寸法に合わせる）。
	sup.OnFormat = func(w, h int) {
		full, _ := config.Load()
		full.Client.CamWidth, full.Client.CamHeight = w, h
		_ = config.Save(full)
	}
	if err := sup.Start(bin, live, phFn, c.ListenPort, c.CamWidth, c.CamHeight); err != nil {
		fmt.Fprintln(os.Stderr, "[lancast] 起動失敗:", err)
		return 1
	}
	logf("待機映像で仮想カメラを起動しました。Discord でカメラ「MacScreen」を選択（いつ開いてもOK）。")
	logf("ポート %d を監視中…ホスト送出を検出すると自動でライブへ切り替わります。", c.ListenPort)
	logf("停止するには Ctrl-C。")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	logf("停止シグナル受信、停止中…")
	sup.Stop()
	logf("終了")
	return 0
}

// waitStopped はプロセス停止を最大 3.5 秒待ち、停止できたら true を返す。
func waitStopped(r *runner.Runner) bool {
	for i := 0; i < 35 && r.Running(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	return !r.Running()
}

// settingHint は設定エラー時に、どのフラグで直すかのヒントを返す。
func settingHint(mode Mode) string {
	if mode == ModeClient {
		return "（-port / -device / -fifo で指定）"
	}
	return "（-dest / -port / -size / -fps / -bitrate / -encoder / -source で指定）"
}

func printChecks(res deps.Result) {
	for _, c := range res.Checks {
		mark := "OK"
		if !c.OK {
			mark = "NG"
		}
		logf("[%s] %s — %s", mark, c.Name, c.Detail)
		if !c.OK && c.Fix != "" {
			logf("      解消: %s", c.Fix)
		}
	}
}
