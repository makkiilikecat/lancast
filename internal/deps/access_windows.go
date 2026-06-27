//go:build windows

package deps

// canWrite は Windows では使用されない（v4l2loopback は Linux 専用で、
// CheckClient は非 Linux で早期 return する）。リンクのためのスタブ。
func canWrite(dev string) bool { return false }
