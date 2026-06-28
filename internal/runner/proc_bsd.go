//go:build !windows && !linux

package runner

import (
	"os/exec"
	"syscall"
)

// configureProc は ffmpeg を独立プロセスグループで起動し、GUI 側のシグナルから隔離する。
// macOS 等には Pdeathsig が無くカーネルレベルの安全網が張れないため、親(lancast)の異常終了
// (SIGKILL・強制終了・OOM 等)に対しては Start が別途起動する watchdog プロセス
// (spawnWatchdog / RunWatchdog, watchdog_darwin.go)が ffmpeg を道連れに停止する。
// 正常終了経路では Stop() でも停止する（二重の保険）。
func configureProc(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
