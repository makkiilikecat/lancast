//go:build linux

package runner

import (
	"os/exec"
	"syscall"
)

// configureProc は ffmpeg を独立プロセスグループで起動して GUI 側のシグナルから
// 隔離しつつ、Pdeathsig により「親プロセス（lancast）が死んだら ffmpeg もカーネルが
// 自動的に SIGKILL する」安全網を張る。
//
// これが OOM kill や SIGKILL のような後始末コードが一切走らない強制終了でも効く唯一の
// 仕組み。これにより lancast が異常終了しても ffmpeg が孤児として生き残り、UDP ポートや
// /dev/videoN を掴み続け、次回起動が exit 231（bind 失敗）で即死する事故を防ぐ。
//
// 注意: Pdeathsig は厳密には「fork した OS スレッド」の終了で発火する。Go ランタイムは
// アイドルスレッドを park（破棄ではなく）するため通常の運用では誤発火しないが、原理的な
// 早期発火の余地は残る。正常終了経路では別途 Stop() でも停止しており、二重の保険になる。
func configureProc(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
}
