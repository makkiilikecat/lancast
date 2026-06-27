// Package cli はヘッドレス（GUI なし）での Host/Client 実行を提供する。
// SSH 越しの自動化・統合テスト・常駐起動に使う。
package cli

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
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

	r := runner.New()
	// frame= 進捗は流れすぎるため 2 秒に 1 回に間引き、最初の 1 回で「稼働中」を明示する。
	streamUp := false
	var lastFrame time.Time
	r.OnLine = func(s string) {
		if strings.HasPrefix(s, "frame=") {
			if !streamUp {
				streamUp = true
				logf("ストリーム稼働中（映像フレームを確認）")
			}
			now := time.Now()
			if now.Sub(lastFrame) < 2*time.Second {
				return
			}
			lastFrame = now
		}
		fmt.Println(s)
	}
	if err := r.Start(bin, args); err != nil {
		fmt.Fprintln(os.Stderr, "[lancast] 起動失敗:", err)
		return 1
	}
	switch mode {
	case ModeClient:
		logf("ポート %d で受信待機中…（送信側 lancast -host の開始を待っています）", cfg.Client.ListenPort)
		logf("受信開始後に Discord を開き、カメラ「MacScreen」を選択してください。")
	case ModeHost:
		logf("%s:%d へ送信開始（UDP のため受信側の有無は検知できません。受信側を先に起動してください）", cfg.Host.DestIP, cfg.Host.DestPort)
	}
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
