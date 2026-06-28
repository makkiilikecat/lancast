package runner

import (
	"testing"
	"time"
)

func TestIsProgressLine(t *testing.T) {
	cases := map[string]bool{
		"frame=  123 fps= 30 q=-1.0 size=N/A time=00:00:04.10": true,
		"  frame=1 fps=0.0":            true,
		"$ ffmpeg -i udp://@:5004":     false,
		"[終了] 正常終了":                    false,
		"Stream #0:0: Video: rawvideo": false,
	}
	for line, want := range cases {
		if got := isProgressLine(line); got != want {
			t.Errorf("isProgressLine(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestProgressStalled(t *testing.T) {
	now := time.Now()
	if progressStalled(now.Add(-1*time.Second), now, supStallTimeout) {
		t.Error("1秒前の進捗で停滞判定されてはいけない")
	}
	if !progressStalled(now.Add(-10*time.Second), now, supStallTimeout) {
		t.Error("10秒前の進捗なら停滞と判定すべき")
	}
}

func TestParseVideoDims(t *testing.T) {
	cases := []struct {
		line string
		w, h int
		ok   bool
	}{
		{"  Stream #0:0: Video: hevc (Main), yuv420p(tv), 1152x720, q=2-31", 1152, 720, true},
		{"Stream #0:0: Video: rawvideo, yuv420p, 1920x1080, 60 fps", 1920, 1080, true},
		{"frame=  123 fps= 30 q=-1.0 size=N/A", 0, 0, false}, // Video: を含まない
		{"Stream #0:0: Audio: aac, 48000 Hz", 0, 0, false},
	}
	for _, c := range cases {
		w, h, ok := parseVideoDims(c.line)
		if ok != c.ok || (ok && (w != c.w || h != c.h)) {
			t.Errorf("parseVideoDims(%q)=(%d,%d,%v) want (%d,%d,%v)", c.line, w, h, ok, c.w, c.h, c.ok)
		}
	}
}

func TestSupervisorStartStopIdempotent(t *testing.T) {
	s := NewClientSupervisor()
	// 存在しない bin を使い、起動失敗しても loop は回り続ける（待機の再試行）。
	// ここでは Running と Stop の整合のみ検証する（実 ffmpeg は起動しない）。
	if s.Running() {
		t.Fatal("初期状態で Running であってはならない")
	}
	ph := func(w, h int) []string { return []string{"-y"} }
	if err := s.Start("/nonexistent/ffmpeg", []string{"-x"}, ph, 0, 1280, 720); err != nil {
		t.Fatalf("Start が失敗: %v", err)
	}
	if !s.Running() {
		t.Error("Start 後は Running であるべき")
	}
	if err := s.Start("/nonexistent/ffmpeg", nil, ph, 0, 0, 0); err == nil {
		t.Error("二重 Start はエラーを返すべき")
	}
	s.Stop()
	if s.Running() {
		t.Error("Stop 後は Running であってはならない")
	}
}
