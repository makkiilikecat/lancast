//go:build darwin

package ui

import "os/exec"

// openScreenRecordingSettings は macOS の「画面収録」プライバシー設定を開く。
func openScreenRecordingSettings() {
	_ = exec.Command("open", "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture").Start()
}
