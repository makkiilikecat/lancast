package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultConfigFor(t *testing.T) {
	mac := DefaultConfigFor("darwin")
	if mac.Host.Backend != "avfoundation" || mac.Host.Encoder != "hevc_videotoolbox" {
		t.Errorf("darwin 既定値が不正: %+v", mac.Host)
	}
	lin := DefaultConfigFor("linux")
	if lin.Host.Backend != "x11grab" || lin.Host.Encoder != "libx264" {
		t.Errorf("linux 既定値が不正: %+v", lin.Host)
	}
	win := DefaultConfigFor("windows")
	if win.Host.Backend != "gdigrab" {
		t.Errorf("windows 既定値が不正: %+v", win.Host)
	}
	// 共通既定値。
	if mac.Host.Width != 1280 || mac.Host.Height != 720 || mac.Host.FPS != 30 || mac.Host.Bitrate != 20000 {
		t.Errorf("共通既定値が不正: %+v", mac.Host)
	}
	if mac.Client.OutputDevice != "/dev/video10" || mac.Client.ListenPort != 5004 {
		t.Errorf("client 既定値が不正: %+v", mac.Client)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir) // os.UserConfigDir は linux で XDG を参照
	t.Setenv("HOME", dir)            // darwin フォールバック用

	p, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if !filepath.IsAbs(p) {
		t.Fatalf("Path が絶対パスでない: %s", p)
	}

	cfg := DefaultConfig()
	cfg.Host.Bitrate = 12345
	cfg.Client.OutputDevice = "/dev/video20"
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Host.Bitrate != 12345 || got.Client.OutputDevice != "/dev/video20" {
		t.Errorf("ラウンドトリップ不一致: %+v", got)
	}
}

func TestHostValidate(t *testing.T) {
	ok := DefaultConfigFor("darwin").Host
	if msg := ok.Validate(); msg != "" {
		t.Errorf("既定 Host が不正と判定された: %s", msg)
	}
	bad := ok
	bad.Width = 0
	if ok.Validate() != "" || bad.Validate() == "" {
		t.Error("幅0 が検出されない")
	}
	bad = ok
	bad.DestPort = 70000
	if bad.Validate() == "" {
		t.Error("範囲外ポートが検出されない")
	}
	bad = ok
	bad.Source = ""
	if bad.Validate() == "" {
		t.Error("空のキャプチャ入力が検出されない")
	}
}

func TestClientValidate(t *testing.T) {
	ok := DefaultConfigFor("linux").Client
	if msg := ok.Validate(); msg != "" {
		t.Errorf("既定 Client が不正と判定された: %s", msg)
	}
	bad := ok
	bad.FifoSize = 0
	if bad.Validate() == "" {
		t.Error("fifo_size 0 が検出されない")
	}
	bad = ok
	bad.ListenPort = 0
	if bad.Validate() == "" {
		t.Error("ポート0 が検出されない")
	}
}

func TestLoadMissingReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Host.Width != 1280 {
		t.Errorf("既定値が返っていない: %+v", got.Host)
	}
}
