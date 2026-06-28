package runner

// WatchdogArg は自分自身を「親監視プロセス」として re-exec する際の第1引数マーカ。
// main がこれを検出したら RunWatchdog を実行して即終了する（GUI もヘッドレスも起動しない）。
//
// 背景: macOS には Linux の Pdeathsig 相当が無く、親(lancast)が SIGKILL・強制終了・OOM・
// パニック後の os.Exit など後始末コードを一切走らせずに死ぬと、子の ffmpeg が孤児として
// 生き残り UDP ポートや仮想カメラを掴み続ける。アプリ内 goroutine は親と一緒に死ぬため
// 役に立たない。唯一の対策は「親より長生きする別プロセス」で親の終了を監視することで、
// それを担うのが本 watchdog（darwin のみ。Linux/Windows は spawnWatchdog が no-op）。
const WatchdogArg = "__lancast_watchdog__"
