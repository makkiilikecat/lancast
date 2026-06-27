//go:build darwin

// Package display は実画面の解像度・縦横比を取得する。
// アナモルフィック送出（実画面比を SAR として埋め込む）に使う。
package display

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <CoreGraphics/CoreGraphics.h>
*/
import "C"

// MainAspect はメインディスプレイの (幅, 高さ, 取得成否) を返す。
// 画面キャプチャは正方ピクセル前提なので、この比がそのまま表示比となる。
func MainAspect() (w, h int, ok bool) {
	id := C.CGMainDisplayID()
	w = int(C.CGDisplayPixelsWide(id))
	h = int(C.CGDisplayPixelsHigh(id))
	if w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}
