//go:build !darwin

// Package macperm は macOS 以外では何もしない。
package macperm

// Supported はこの OS で許可状態の判定が可能かを返す。
func Supported() bool { return false }

// Granted は macOS 以外では常に true（許可の概念がない）。
func Granted() bool { return true }

// Request は macOS 以外では何もしない。
func Request() bool { return true }
