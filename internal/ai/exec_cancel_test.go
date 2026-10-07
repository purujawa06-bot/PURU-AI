package ai

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// sleepCmd blocks ~30s on any platform so cancel tests can prove the
// parent context stops a blocking run (unix: sleep, windows: ping count).
func sleepCmd() string {
	if runtime.GOOS == "windows" {
		return "ping -n 30 127.0.0.1 >nul"
	}
	return "sleep 30"
}

// Cancel from /stop (parent ctx) must stop a blocking run quickly — not by
// waiting out its own timeout. Not a timeout: TimedOut must be false so the
// diagnosis stays truthful.
func TestRunExecParentCancelStopsBlocking(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(500*time.Millisecond, cancel)
	start := time.Now()
	res, _ := runExec(ctx, t.TempDir(), sleepCmd(), 120, 64, false)
	if elapsed := time.Since(start); elapsed > 60*time.Second {
		t.Fatalf("cancel did not stop blocking run: %v", elapsed)
	}
	m, _ := res.(execResult)
	if m.Success {
		t.Fatalf("cancelled run should fail: %+v", res)
	}
	if m.TimedOut {
		t.Fatalf("cancel user bukan timeout: %+v", res)
	}
}

// Background sessions live across requests (polled/killed later), so a
// request cancel (/stop) must not kill them — use kill instead.
func TestRunExecBackgroundSurvivesParentCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, _ := runExec(ctx, t.TempDir(), sleepCmd(), 120, 64, true)
	m, _ := out.(map[string]any)
	sid, _ := m["sessionId"].(string)
	if m["success"] != true || sid == "" {
		t.Fatalf("background with parent cancel should still run: %v", out)
	}
	defer killExec(sid)
	p, _ := pollExec(sid)
	pm, _ := p.(map[string]any)
	if pm["status"] != "running" {
		t.Fatalf("background must stay running despite parent cancel: %v", p)
	}
	if _, err := killExec(sid); err != nil {
		t.Fatal(err)
	}
}
