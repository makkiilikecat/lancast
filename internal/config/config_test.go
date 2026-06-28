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
	// 仮想カメラの既定提示 fps はホスト既定 fps に一致する（不定 framerate を避ける）。
	if mac.Client.FPS != mac.Host.FPS {
		t.Errorf("client 既定 FPS=%d は host 既定 FPS=%d に一致すべき", mac.Client.FPS, mac.Host.FPS)
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
	bad = ok
	bad.FPS = -1
	if bad.Validate() == "" {
		t.Error("負の FPS が検出されない")
	}
	good := ok
	good.FPS = 0 // 0=ソースのまま は許容
	if good.Validate() != "" {
		t.Error("FPS=0(ソースのまま) は許容されるべき")
	}
	bad = ok
	bad.OutputMode = "weird"
	if bad.Validate() == "" {
		t.Error("不正な出力モードが検出されない")
	}
	bad = ok
	bad.OutputMode = OutputFixed
	bad.CamWidth = 0
	if bad.Validate() == "" {
		t.Error("固定モードでカメラ幅0 が検出されない")
	}
	good = ok
	good.OutputMode = OutputFollow
	good.CamWidth, good.CamHeight = 0, 0 // follow ではカメラ解像度不要
	if good.Validate() != "" {
		t.Error("follow モードはカメラ解像度0でも許容されるべき")
	}
}

func TestDefaultClientOutputMode(t *testing.T) {
	c := DefaultConfigFor("linux").Client
	if c.OutputMode != OutputFixed || c.CamWidth != 1920 || c.CamHeight != 1080 {
		t.Errorf("既定の Client 出力モードが不正: %+v", c)
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
