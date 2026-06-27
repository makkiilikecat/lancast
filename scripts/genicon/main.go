// genicon は LANCast のアプリアイコン(assets/icon.png)を生成する。
// 依存を増やさないため標準ライブラリのみで描画する。
// 使い方: go run ./scripts/genicon > /dev/null （出力先は assets/icon.png 固定）
package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

const (
	size   = 1024
	radius = 200.0
)

func main() {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	bg := color.RGBA{0x2e, 0x5b, 0xd6, 0xff} // UI のボタン色に合わせた青
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}

	// 三角形(再生/配信を示すプレイマーク)の頂点。
	ax, ay := 400.0, 312.0
	bx, by := 400.0, 712.0
	cx, cy := 740.0, 512.0

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if !insideRounded(float64(x), float64(y)) {
				continue // 角丸の外は透明のまま
			}
			if inTriangle(float64(x), float64(y), ax, ay, bx, by, cx, cy) {
				img.Set(x, y, white)
			} else {
				img.Set(x, y, bg)
			}
		}
	}

	f, err := os.Create("assets/icon.png")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}

// insideRounded は角丸正方形の内側か判定する。
func insideRounded(x, y float64) bool {
	minX, minY := radius, radius
	maxX, maxY := size-radius, size-radius
	cx := math.Max(minX, math.Min(x, maxX))
	cy := math.Max(minY, math.Min(y, maxY))
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= radius*radius
}

// inTriangle は点(px,py)が三角形 abc の内側か判定する。
func inTriangle(px, py, ax, ay, bx, by, cx, cy float64) bool {
	d1 := sign(px, py, ax, ay, bx, by)
	d2 := sign(px, py, bx, by, cx, cy)
	d3 := sign(px, py, cx, cy, ax, ay)
	hasNeg := d1 < 0 || d2 < 0 || d3 < 0
	hasPos := d1 > 0 || d2 > 0 || d3 > 0
	return !(hasNeg && hasPos)
}

func sign(px, py, ax, ay, bx, by float64) float64 {
	return (px-bx)*(ay-by) - (ax-bx)*(py-by)
}
