//go:build !darwin

// Package display は macOS 以外では画面比を検出できない（呼び出し側でフォールバック）。
package display

// MainAspect は macOS 以外では取得不可（ok=false）を返す。
func MainAspect() (w, h int, ok bool) { return 0, 0, false }
