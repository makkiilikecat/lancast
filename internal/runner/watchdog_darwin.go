//go:build darwin

package runner

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// spawnWatchdog は「親(この lancast プロセス)が死んだら childPid の ffmpeg を道連れに
// SIGKILL する」監視プロセスを起動する。自分自身(同一バイナリ)を WatchdogArg 付きで
// re-exec し、RunWatchdog を実行させる。
//
// childPid は ffmpeg のリーダ pid（configureProc が Setpgid 済みのため pgid と一致）。
// 返す関数は監視プロセスを停止・回収する（ffmpeg を正常停止させたあとに呼ぶ）。
func spawnWatchdog(childPid int) func() {
	exe, err := os.Executable()
	if err != nil {
		return func() {}
	}
	cmd := exec.Command(exe, WatchdogArg, strconv.Itoa(os.Getpid()), strconv.Itoa(childPid))
	// 独立プロセスグループ: ffmpeg グループへの SIGINT/SIGKILL に巻き込まれない。
	// 親(lancast)が死ねば launchd へ里子に出されつつ、kqueue で検知して動作を続ける。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return func() {}
	}
	go func() { _ = cmd.Wait() }() // 終了時のゾンビ化を防ぐ
	return func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
}

// RunWatchdog は ppid(親 lancast) と cpid(ffmpeg) の終了を待ち受ける。
// 親が先に死ねば ffmpeg のプロセスグループを SIGKILL する。ffmpeg が先に死ねば
// 何もせず終了する。WatchdogArg 付きで起動された専用プロセスから main 経由で呼ばれる。
//
// 「NOTE_EXIT による即時検知」と「1 秒ごとのバックストップ poll」の二段構え:
//   - 通常は kqueue がプロセス終了を即座に通知するので反応は瞬時。
//   - 登録より前に親/子が既に終了していた競合や、ゾンビ化など kqueue が取りこぼし得る
//     状況でも、最長 1 秒のバックストップが確実に拾う。
//
// 親(lancast)の判定には「Getppid() が起動時の親 pid(ppid)から変わったか」を使う。
// watchdog は lancast の実子なので、親がどんな死に方をしても孤児となり再親付けされ、
// getppid が必ず変化する。macOS では孤児の里親が必ずしも pid 1 ではない(セッションの
// launchd 等になる)ため ==1 では判定できないが、「元の親と異なる」なら里親が誰であれ
// 確実。kill(pid,0) がゾンビを生存と誤判定する問題も pid 再利用も受けない。
func RunWatchdog(ppid, cpid int) {
	kq, err := unix.Kqueue()
	if err != nil {
		pollWatch(ppid, cpid) // kqueue が使えない環境向けの保険
		return
	}
	defer unix.Close(kq)

	// 親・子の両方に NOTE_EXIT を登録（best-effort。失敗しても下の生存確認が担保する）。
	changes := []unix.Kevent_t{procExitKevent(ppid), procExitKevent(cpid)}
	_, _ = unix.Kevent(kq, changes, nil, nil)

	events := make([]unix.Kevent_t, len(changes))
	timeout := unix.Timespec{Sec: 1} // poll バックストップの間隔
	for {
		// 即時検知(下の Kevent)に先んじて、まず確実なバックストップで競合・取りこぼしを潰す。
		if unix.Getppid() != ppid {
			killGroup(cpid) // 親(lancast)が消えて孤児化(再親付け)した → ffmpeg を道連れに
			return
		}
		if !alive(cpid) {
			return // ffmpeg が先に終了 → 何もしない
		}
		n, err := unix.Kevent(kq, nil, events, &timeout)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			pollWatch(ppid, cpid)
			return
		}
		for i := 0; i < n; i++ {
			switch int(events[i].Ident) {
			case ppid:
				killGroup(cpid)
				return
			case cpid:
				return
			}
		}
		// n==0 はタイムアウト。ループ先頭の生存確認へ戻る。
	}
}

// alive は pid が存在するか（シグナル 0 送出が成功するか）を返す。
func alive(pid int) bool { return unix.Kill(pid, 0) == nil }

// procExitKevent は指定 pid のプロセス終了(NOTE_EXIT)を1回だけ通知する kevent を作る。
func procExitKevent(pid int) unix.Kevent_t {
	return unix.Kevent_t{
		Ident:  uint64(pid),
		Filter: unix.EVFILT_PROC,
		Flags:  unix.EV_ADD | unix.EV_ONESHOT,
		Fflags: unix.NOTE_EXIT,
	}
}

// killGroup は pid を pgid とするプロセスグループ全体を SIGKILL する。
func killGroup(pid int) {
	_ = unix.Kill(-pid, unix.SIGKILL)
}

// pollWatch は kqueue が使えない場合の保険。親(lancast)の孤児化を定期確認し、親が
// 消えたら ffmpeg を道連れに、ffmpeg が先に消えたら終了する。
func pollWatch(ppid, cpid int) {
	for {
		if unix.Getppid() != ppid {
			killGroup(cpid)
			return
		}
		if !alive(cpid) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}
