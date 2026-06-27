//go:build !windows && !linux

package runner

import (
	"os/exec"
	"syscall"
)

// configureProc は ffmpeg を独立プロセスグループで起動し、GUI 側のシグナルから隔離する。
// macOS 等には Pdeathsig が無いため、親の異常終了に対するカーネルレベルの安全網は無い。
// 正常終了経路の Stop() と、起動前チェックでクリーンアップする。
func configureProc(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
