// lancast は LAN 内で画面を送受信し、受信側を仮想カメラ(v4l2loopback)へ流す
// Gio 製の単一バイナリ GUI アプリ。Host(送信)/Client(受信) を同一インスタンスで扱える。
package main

import (
	"log"
	"os"

	"gioui.org/app"
	"gioui.org/unit"

	"lancast/internal/ui"
)

func main() {
	go func() {
		a := ui.NewApp()
		w := new(app.Window)
		w.Option(app.Title("LANCast"), app.Size(unit.Dp(740), unit.Dp(860)))
		if err := a.Run(w); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}
