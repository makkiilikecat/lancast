// Package preview は ffmpeg が吐く MJPEG ストリームを受け取り、
// 最新フレームを画像として保持する。GUI のプレビュー表示に使う。
//
// 本配信/受信とは別プロセスの ffmpeg を起動し、MJPEG を TCP(127.0.0.1)
// へ送らせる。本ストリームを巻き込まずにプレビューだけをオン/オフできる。
package preview

import (
	"bufio"
	"bytes"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"net"
	"sync"

	"lancast/internal/runner"
)

// ArgsFunc は割り当てられた TCP URL を受け取り、ffmpeg 引数列を返す。
type ArgsFunc func(url string) []string

// Preview は1本のプレビュー用 ffmpeg プロセスと最新フレームを管理する。
type Preview struct {
	mu     sync.Mutex
	frame  *image.RGBA
	ln     net.Listener
	runner *runner.Runner
	onUpd  func() // フレーム更新時の通知（UI 再描画）
}

// New はプレビューを生成する。onUpdate は新フレーム到着時に呼ばれる。
func New(onUpdate func()) *Preview {
	return &Preview{runner: runner.New(), onUpd: onUpdate}
}

// Running はプレビュー用 ffmpeg が稼働中か返す。
func (p *Preview) Running() bool { return p.runner.Running() }

// Frame は最新フレーム（無ければ nil）を返す。呼び出し側は描画のみに使うこと。
func (p *Preview) Frame() *image.RGBA {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.frame
}

// Start はローカル TCP を開いて待ち受け、argsFor(url) で ffmpeg を起動する。
// p.ln を稼働中フラグ兼ねて mu 配下で扱い、二重起動・リスナ/goroutine の
// リークを防ぐ（ln!=nil の間は起動済みとみなす）。
func (p *Preview) Start(bin string, argsFor ArgsFunc) error {
	p.mu.Lock()
	if p.ln != nil {
		p.mu.Unlock()
		return nil // 既に稼働中
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		p.mu.Unlock()
		return err
	}
	p.ln = ln
	p.mu.Unlock()

	url := "tcp://" + ln.Addr().String()
	go p.accept(ln)

	if err := p.runner.Start(bin, argsFor(url)); err != nil {
		p.mu.Lock()
		if p.ln == ln {
			p.ln = nil
		}
		p.mu.Unlock()
		_ = ln.Close()
		return err
	}
	return nil
}

// Stop はプレビュー用 ffmpeg を停止し、リスナを閉じる。
func (p *Preview) Stop() {
	p.mu.Lock()
	ln := p.ln
	p.ln = nil
	p.mu.Unlock()

	p.runner.Stop()
	if ln != nil {
		_ = ln.Close() // accept の Accept がエラーで抜ける
	}
	p.mu.Lock()
	p.frame = nil
	p.mu.Unlock()
}

func (p *Preview) accept(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // Stop でクローズ
		}
		p.readFrames(conn, p.decode)
		_ = conn.Close()
	}
}

// readFrames は連結された JPEG ストリームを SOI(FFD8)〜EOI(FFD9) で切り出す。
// JPEG はバイトスタッフィング（圧縮データ中の FF は必ず FF00）により、
// FFD8/FFD9 が実マーカ以外で現れないため、この単純な走査で安全に分割できる。
func (p *Preview) readFrames(r io.Reader, onJPEG func([]byte)) {
	br := bufio.NewReaderSize(r, 1<<20)
	var buf []byte
	for {
		b, err := br.ReadByte()
		if err != nil {
			return
		}
		buf = append(buf, b)
		n := len(buf)
		if n >= 2 && buf[n-2] == 0xFF && buf[n-1] == 0xD9 {
			if i := bytes.Index(buf, []byte{0xFF, 0xD8}); i >= 0 {
				onJPEG(buf[i:])
			}
			buf = buf[:0]
		}
		if len(buf) > 16<<20 { // 暴走防止（壊れたストリーム）
			buf = buf[:0]
		}
	}
}

func (p *Preview) decode(jpg []byte) {
	img, err := jpeg.Decode(bytes.NewReader(jpg))
	if err != nil {
		return
	}
	// Gio へ確実に渡せるよう RGBA へ変換する（jpeg は通常 YCbCr）。
	b := img.Bounds()
	rgba := image.NewRGBA(b)
	draw.Draw(rgba, b, img, b.Min, draw.Src)

	p.mu.Lock()
	p.frame = rgba
	p.mu.Unlock()
	if p.onUpd != nil {
		p.onUpd()
	}
}
