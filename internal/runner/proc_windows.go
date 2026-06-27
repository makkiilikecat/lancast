//go:build windows

package runner

import "os/exec"

// configureProc は Windows では特別な設定を行わない。
func configureProc(cmd *exec.Cmd) {}

// interruptCmd は Windows では SIGINT 相当が無いため Kill する。
func interruptCmd(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// killCmd はプロセスを強制終了する。
func killCmd(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
