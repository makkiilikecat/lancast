//go:build darwin

// Package macperm は macOS の画面収録許可状態を扱う。
// CoreGraphics の公開 API（10.15+）を使い、モーダルを出さずに状態を確認できる。
package macperm

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <CoreGraphics/CoreGraphics.h>
*/
import "C"

// Supported はこの OS で許可状態の判定が可能かを返す。
func Supported() bool { return true }

// Granted は画面収録が許可済みかを返す。モーダルは出さない
// （CGPreflightScreenCaptureAccess）。
func Granted() bool {
	return bool(C.CGPreflightScreenCaptureAccess())
}

// Request は未許可の場合にシステムの許可モーダルを表示する
// （CGRequestScreenCaptureAccess）。許可済みなら何もしない。
// 戻り値は呼び出し直後の許可状態。初回許可は反映にアプリ再起動を要する。
func Request() bool {
	return bool(C.CGRequestScreenCaptureAccess())
}
