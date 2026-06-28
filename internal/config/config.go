// Package config はアプリの永続設定（Host/Client 双方のパラメータ）を扱う。
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

// HostConfig は送信側（画面キャプチャ → ネットワーク送出）の設定。
type HostConfig struct {
	Backend       string `json:"backend"`        // avfoundation | x11grab | gdigrab
	Source        string `json:"source"`         // 例: "3:none"(mac) ":0.0"(linux) "desktop"(win)
	CaptureCursor bool   `json:"capture_cursor"` // カーソルを含めるか
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	FPS           int    `json:"fps"`
	Bitrate       int    `json:"bitrate_kbps"` // kbps
	Encoder       string `json:"encoder"`
	DestIP        string `json:"dest_ip"`
	DestPort      int    `json:"dest_port"`
	ExtraArgs     string `json:"extra_args"` // 追加 ffmpeg 引数（空白区切り）

	// TargetAspect はプリセット（縦解像度 → 横ドット数）の算出に使う基準比。
	// 例 "16:9" "16:10"。"" は検出した実画面比を使う。送出自体は Width:Height を
	// そのまま使う（黒帯やアナモルフィックはしない＝WYSIWYG）。
	TargetAspect string `json:"target_aspect"`
}

// ClientConfig は受信側（ネットワーク受信 → 仮想カメラ書き込み）の設定。
//
// 受信は無加工。ホストが確定した映像（解像度・FPS・アスペクト・ピクセル形式）を
// そのまま v4l2 仮想カメラへ流す。スケール・黒帯・FPS 正規化・アスペクト復元は一切しない。
type ClientConfig struct {
	ListenPort   int    `json:"listen_port"`
	OutputDevice string `json:"output_device"` // 例: /dev/video10
	FifoSize     int    `json:"fifo_size"`     // UDP 受信バッファ
	LowDelay     bool   `json:"low_delay"`     // nobuffer + low_delay
	ExtraArgs    string `json:"extra_args"`

	// CamWidth/CamHeight は待機(プレースホルダ)映像の解像度。ユーザー設定ではなく、
	// 直近に受信したホスト送出の解像度を学習して保持する。待機⇄ライブで仮想カメラの
	// フォーマット（特に幅＝ストライド）を一致させ、Discord 側の斜めズレ（シアー）を
	// 防ぐためにある。既定は 1280x720 で、初回受信前のみ既定値が使われる。
	CamWidth  int `json:"cam_width"`
	CamHeight int `json:"cam_height"`
}

// Config はアプリ全体の設定。
type Config struct {
	Host   HostConfig   `json:"host"`
	Client ClientConfig `json:"client"`
}

// DefaultConfig は実行 OS に応じた既定値を返す。
// 既定値は本セッションで確立した Mac(host)→Ubuntu(client) 構成を反映する。
func DefaultConfig() Config {
	return DefaultConfigFor(runtime.GOOS)
}

// DefaultConfigFor は指定 OS 向けの既定値を返す（テスト容易性のため分離）。
func DefaultConfigFor(goos string) Config {
	host := HostConfig{
		CaptureCursor: true,
		Width:         1280,
		Height:        720,
		FPS:           30,
		Bitrate:       20000,
		DestIP:        "192.168.0.215",
		DestPort:      5004,
	}
	switch goos {
	case "darwin":
		host.Backend = "avfoundation"
		host.Source = "3:none" // Capture screen 0（内蔵ディスプレイ）
		host.Encoder = "hevc_videotoolbox"
	case "linux":
		host.Backend = "x11grab"
		host.Source = ":0.0"
		host.Encoder = "libx264"
	case "windows":
		host.Backend = "gdigrab"
		host.Source = "desktop"
		host.Encoder = "libx264"
	default:
		host.Backend = "avfoundation"
		host.Source = "3:none"
		host.Encoder = "libx264"
	}
	client := ClientConfig{
		ListenPort:   5004,
		OutputDevice: "/dev/video10",
		FifoSize:     1000000,
		LowDelay:     true,
		// 待機映像の既定解像度。初回受信でホスト送出の実寸へ学習・更新される。
		CamWidth:  1280,
		CamHeight: 720,
	}
	return Config{Host: host, Client: client}
}

// Validate は Host 設定が ffmpeg 起動に足る妥当な値か検査する。
// 不正なら理由を返す（空欄→0 などの無効値での起動を防ぐ）。
func (h HostConfig) Validate() string {
	switch {
	case h.Width <= 0 || h.Height <= 0:
		return "解像度(幅・高さ)を 1 以上で指定してください"
	case h.FPS <= 0:
		return "FPS を 1 以上で指定してください"
	case h.Bitrate <= 0:
		return "ビットレートを 1 以上で指定してください"
	case h.DestPort <= 0 || h.DestPort > 65535:
		return "ポートは 1〜65535 で指定してください"
	case h.DestIP == "":
		return "送信先 IP を入力してください"
	case h.Source == "":
		return "キャプチャ入力を指定してください"
	case h.Encoder == "":
		return "エンコーダを選択してください"
	case !validTargetAspect(h.TargetAspect):
		return "目標比率の指定が不正です"
	}
	return ""
}

// TargetAspects は TargetAspect に指定できる値（"" = 画面そのまま＝先頭）。
// 横長に続けて縦長を並べる。UI のセレクタ順にも使う。
var TargetAspects = []string{"", "16:9", "16:10", "21:9", "3:2", "4:3", "5:3", "9:16", "9:21", "2:3", "3:4", "3:5"}

func validTargetAspect(s string) bool {
	for _, a := range TargetAspects {
		if a == s {
			return true
		}
	}
	return false
}

// Validate は Client 設定が妥当か検査する。
func (c ClientConfig) Validate() string {
	switch {
	case c.ListenPort <= 0 || c.ListenPort > 65535:
		return "受信ポートは 1〜65535 で指定してください"
	case c.FifoSize <= 0:
		return "バッファ(fifo_size)を 1 以上で指定してください"
	case c.OutputDevice == "":
		return "出力デバイスを入力してください"
	}
	return ""
}

// Path は設定ファイルの保存先パスを返す。
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lancast", "config.json"), nil
}

// Load は設定を読み込む。ファイルが無ければ既定値を返す。
func Load() (Config, error) {
	p, err := Path()
	if err != nil {
		return DefaultConfig(), err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return DefaultConfig(), err
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(b, &cfg); err != nil {
		// 破損設定は .bak へ退避してから既定値を返す（次回 Save での無言上書き＝消失を防ぐ）。
		_ = os.Rename(p, p+".bak")
		return DefaultConfig(), err
	}
	return cfg, nil
}

// Save は設定を JSON として保存する。
func Save(cfg Config) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}
