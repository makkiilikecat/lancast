// Package cli はヘッドレス（GUI なし）での Host/Client 実行を提供する。
// SSH 越しの自動化・統合テスト・常駐起動に使う。
package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lancast/internal/config"
	"lancast/internal/deps"
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
	fmt.Printf("[lancast] "+format+"\n", a...)
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
		fmt.Fprintln(os.Stderr, "[lancast] 設定エラー:", valid)
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

	r := runner.New()
	r.OnLine = func(s string) { fmt.Println(s) }
	if err := r.Start(bin, args); err != nil {
		fmt.Fprintln(os.Stderr, "[lancast] 起動失敗:", err)
		return 1
	}
	logf("起動しました。停止するには Ctrl-C。")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	for {
		select {
		case <-sig:
			logf("停止シグナル受信、停止中…")
			r.Stop()
			waitStopped(r)
			logf("終了")
			return 0
		case <-time.After(200 * time.Millisecond):
			if !r.Running() {
				fmt.Fprintln(os.Stderr, "[lancast] ffmpeg が終了しました（上記ログを確認してください）")
				return 1
			}
		}
	}
}

func waitStopped(r *runner.Runner) {
	for i := 0; i < 30 && r.Running(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
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
