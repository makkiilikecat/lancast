// Package runner は ffmpeg プロセスのライフサイクルとログ収集を扱う。
package runner

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const maxLogLines = 500

// Runner は1本の ffmpeg プロセスを管理する（Host用/Client用に各1つ）。
type Runner struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	running bool
	lines   []string

	// OnUpdate はログ更新・状態変化時に呼ばれる（UI 再描画トリガ用）。
	OnUpdate func()
	// OnLine は新しいログ行ごとに呼ばれる（ヘッドレス時の標準出力用）。
	// Start 前に設定し、以後変更しないこと。
	OnLine func(string)
}

// New は Runner を生成する。
func New() *Runner {
	return &Runner{}
}

// Running は実行中か返す。
func (r *Runner) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// Log は現在のログ全文を返す。
func (r *Runner) Log() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.lines, "\n")
}

// Clear はログを消去する。
func (r *Runner) Clear() {
	r.mu.Lock()
	r.lines = nil
	r.mu.Unlock()
	r.notify()
}

func (r *Runner) append(line string) {
	r.mu.Lock()
	r.lines = append(r.lines, line)
	if len(r.lines) > maxLogLines {
		// 先頭を切り捨てつつ、古いバッキング配列を解放するためコピーし直す。
		r.lines = append([]string(nil), r.lines[len(r.lines)-maxLogLines:]...)
	}
	r.mu.Unlock()
	if r.OnLine != nil {
		r.OnLine(line)
	}
	r.notify()
}

func (r *Runner) notify() {
	if r.OnUpdate != nil {
		r.OnUpdate()
	}
}

// Start は bin を args で起動する。既に実行中なら何もしない。
func (r *Runner) Start(bin string, args []string) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return fmt.Errorf("既に実行中です")
	}
	r.mu.Unlock()

	cmd := exec.Command(bin, args...)
	configureProc(cmd)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	r.mu.Lock()
	r.cmd = cmd
	r.running = true
	r.mu.Unlock()
	r.append("$ " + bin + " " + strings.Join(args, " ")) // append が notify も行う

	go r.pump(stderr)
	go r.pump(stdout)
	go r.wait(cmd)
	return nil
}

// pump は ffmpeg の出力を1行ずつ（\r も区切りとして）ログへ流す。
func (r *Runner) pump(rc io.ReadCloser) {
	reader := bufio.NewReader(rc)
	var buf strings.Builder
	for {
		b, err := reader.ReadByte()
		if err != nil {
			if buf.Len() > 0 {
				r.append(buf.String())
			}
			return
		}
		if b == '\n' || b == '\r' {
			if buf.Len() > 0 {
				line := buf.String()
				r.append(line)
				r.maybeHint(line)
				buf.Reset()
			}
			continue
		}
		buf.WriteByte(b)
	}
}

// maybeHint は ffmpeg の代表的なエラーに対し、人間向けの対処ヒントを補う。
func (r *Runner) maybeHint(line string) {
	switch {
	case strings.Contains(line, "Address already in use"):
		r.append("[ヒント] 前回の ffmpeg がポートを掴んだまま残っています。`lsof -iUDP:<port>` で確認し、残プロセスを終了してください。")
	case strings.Contains(line, "Operation not permitted"), strings.Contains(line, "Configuration of video device failed"):
		r.append("[ヒント] macOS は『システム設定>プライバシー>画面収録』で許可が必要です（許可後アプリ再起動）。")
	}
}

func (r *Runner) wait(cmd *exec.Cmd) {
	err := cmd.Wait()
	r.mu.Lock()
	r.running = false
	r.cmd = nil
	r.mu.Unlock()
	if err != nil {
		r.append("[終了] " + err.Error())
	} else {
		r.append("[終了] 正常終了")
	}
	r.notify()
}

// Stop は実行中プロセスを停止する（SIGINT → 2秒後に強制 Kill）。
// シグナルはプロセスグループ全体へ送る（configureProc が Setpgid 済み）。
func (r *Runner) Stop() {
	r.mu.Lock()
	cmd := r.cmd
	running := r.running
	r.mu.Unlock()
	if !running || cmd == nil || cmd.Process == nil {
		return
	}

	interruptCmd(cmd)

	// 2秒後、同一プロセスがまだ動いていれば強制終了する。
	// その間に Stop→Start で別プロセスへ入れ替わっていたら何もしない。
	go func(target *exec.Cmd) {
		time.Sleep(2 * time.Second)
		r.mu.Lock()
		// 同一プロセスがまだ現役なら強制終了。wait() 完了で r.cmd は nil に
		// 入れ替わるため、ポインタ一致のみで判定すれば誤殺・取りこぼしを防げる。
		same := r.cmd == target
		r.mu.Unlock()
		if same {
			killCmd(target)
		}
	}(cmd)
}
