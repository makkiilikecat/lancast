package runner

import (
	"fmt"
	"net"
)

// UDPPortAvailable は指定 UDP ポートが受信用に bind 可能かを確認する。
// 使用中なら error を返す。ffmpeg は udp://@:PORT で同じ wildcard bind を行うため、
// 起動前にここで弾けば exit 231（bind 失敗 → Inappropriate ioctl）を未然に防げる。
//
// 一旦 bind して即座に閉じるだけの非破壊チェック。bind と実際の ffmpeg 起動の間に
// 別プロセスが奪う僅かな競合はありうるが、その場合は従来どおり ffmpeg 側で検出される。
func UDPPortAvailable(port int) error {
	c, err := net.ListenPacket("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	return c.Close()
}
