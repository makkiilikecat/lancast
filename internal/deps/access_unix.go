//go:build !windows

package deps

import "golang.org/x/sys/unix"

// canWrite はデバイスを open せずに書き込み権限のみを確認する。
// v4l2loopback は open 自体が副作用（ストリーム状態変化）を持ちうるため Access を使う。
func canWrite(dev string) bool {
	return unix.Access(dev, unix.W_OK) == nil
}
