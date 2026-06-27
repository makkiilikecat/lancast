package runner

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

func waitUntil(t *testing.T, cond func() bool, d time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

func TestRunner_CapturesOutputAndExits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX シェル前提のためスキップ")
	}
	r := New()
	updated := make(chan struct{}, 64)
	r.OnUpdate = func() {
		select {
		case updated <- struct{}{}:
		default:
		}
	}
	// ffmpeg は stderr にログを出すため、テストでも stderr へ出力する。
	if err := r.Start("sh", []string{"-c", "echo hello 1>&2; echo world"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !waitUntil(t, func() bool { return !r.Running() }, 3*time.Second) {
		t.Fatal("プロセスが終了しない")
	}
	log := r.Log()
	if !strings.Contains(log, "hello") || !strings.Contains(log, "world") {
		t.Errorf("stdout/stderr の両方が記録されていない: %q", log)
	}
	if !strings.Contains(log, "正常終了") {
		t.Errorf("終了マーカーが無い: %q", log)
	}
}

func TestRunner_StopTerminates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX シェル前提のためスキップ")
	}
	r := New()
	if err := r.Start("sh", []string{"-c", "sleep 30"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !waitUntil(t, r.Running, time.Second) {
		t.Fatal("起動を確認できない")
	}
	r.Stop()
	if !waitUntil(t, func() bool { return !r.Running() }, 4*time.Second) {
		t.Fatal("Stop でプロセスが終了しない")
	}
}

func TestRunner_DoubleStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX シェル前提のためスキップ")
	}
	r := New()
	if err := r.Start("sh", []string{"-c", "sleep 5"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Stop()
	if !waitUntil(t, r.Running, time.Second) {
		t.Fatal("起動を確認できない")
	}
	if err := r.Start("sh", []string{"-c", "sleep 5"}); err == nil {
		t.Error("二重起動がエラーにならない")
	}
}

func TestRunner_Clear(t *testing.T) {
	r := New()
	r.append("line1")
	r.append("line2")
	if r.Log() == "" {
		t.Fatal("ログが空")
	}
	r.Clear()
	if r.Log() != "" {
		t.Errorf("Clear 後もログが残る: %q", r.Log())
	}
}
