//go:build !darwin

package runner

// spawnWatchdog は darwin 以外では no-op。Linux は configureProc の Pdeathsig が
// カーネルレベルで ffmpeg を道連れにするため監視プロセスは不要。Windows は将来
// Job Object(JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE)で同等を担保する余地がある。
func spawnWatchdog(childPid int) func() { return func() {} }

// RunWatchdog は darwin 以外では使われない（WatchdogArg は darwin の spawnWatchdog
// からのみ起動されるため）。クロスプラットフォームの main から参照可能にするための空実装。
func RunWatchdog(ppid, cpid int) {}
