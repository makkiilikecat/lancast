package main

import (
	"testing"

	"lancast/internal/config"
)

func TestParseSize(t *testing.T) {
	cases := []struct {
		in   string
		w, h int
		ok   bool
	}{
		{"1280x720", 1280, 720, true},
		{"1920X1080", 1920, 1080, true},
		{" 640 x 480 ", 640, 480, true},
		{"", 0, 0, false},
		{"1280", 0, 0, false},
		{"axb", 0, 0, false},
		{"0x720", 0, 0, false},
	}
	for _, c := range cases {
		w, h, ok := parseSize(c.in)
		if ok != c.ok || w != c.w || h != c.h {
			t.Errorf("parseSize(%q)=(%d,%d,%v) want (%d,%d,%v)", c.in, w, h, ok, c.w, c.h, c.ok)
		}
	}
}

func TestApplyOverrides(t *testing.T) {
	cfg := config.DefaultConfigFor("darwin")
	applyOverrides(&cfg, overrides{
		dest: "10.0.0.5", port: 6000, size: "1920x1080",
		bitrate: 8000, device: "/dev/video20",
	})
	if cfg.Host.DestIP != "10.0.0.5" || cfg.Host.DestPort != 6000 {
		t.Errorf("host override 失敗: %+v", cfg.Host)
	}
	if cfg.Host.Width != 1920 || cfg.Host.Height != 1080 || cfg.Host.Bitrate != 8000 {
		t.Errorf("host サイズ/ビットレート override 失敗: %+v", cfg.Host)
	}
	if cfg.Client.ListenPort != 6000 || cfg.Client.OutputDevice != "/dev/video20" {
		t.Errorf("client override 失敗: %+v", cfg.Client)
	}
}

func TestApplyOverrides_EmptyKeepsDefaults(t *testing.T) {
	cfg := config.DefaultConfigFor("darwin")
	orig := cfg
	applyOverrides(&cfg, overrides{}) // すべて空 → 変更なし
	if cfg != orig {
		t.Errorf("空 override で設定が変わった: %+v", cfg)
	}
}
