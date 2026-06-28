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

func TestSupervisorStartStopIdempotent(t *testing.T) {
	s := NewClientSupervisor()
	// 存在しない bin を使い、起動失敗しても loop は回り続ける（待機の再試行）。
	// ここでは Running と Stop の整合のみ検証する（実 ffmpeg は起動しない）。
	if s.Running() {
		t.Fatal("初期状態で Running であってはならない")
	}
	if err := s.Start("/nonexistent/ffmpeg", []string{"-x"}, []string{"-y"}, 0); err != nil {
		t.Fatalf("Start が失敗: %v", err)
	}
	if !s.Running() {
		t.Error("Start 後は Running であるべき")
	}
	if err := s.Start("/nonexistent/ffmpeg", nil, nil, 0); err == nil {
		t.Error("二重 Start はエラーを返すべき")
	}
	s.Stop()
	if s.Running() {
		t.Error("Stop 後は Running であってはならない")
	}
}
