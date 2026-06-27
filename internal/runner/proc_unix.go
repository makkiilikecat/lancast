//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
)

// interruptCmd はプロセスグループ全体へ SIGINT を送る（ffmpeg を綺麗に終了させる）。
func interruptCmd(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
	}
}

// killCmd はプロセスグループ全体を強制終了する。
func killCmd(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
