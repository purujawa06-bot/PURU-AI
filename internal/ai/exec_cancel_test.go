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

// Cancel dari /stop (parent ctx) harus menghentikan run blocking dengan
// cepat — bukan menunggu timeout sendiri. Bukan timeout: flag TimedOut
// harus false agar diagnosis tidak menyesatkan.
func TestRunExecParentCancelStopsBlocking(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(500*time.Millisecond, cancel)
	start := time.Now()
	res, _ := runExec(ctx, t.TempDir(), sleepCmd(), 120, 64, false)
	if elapsed := time.Since(start); elapsed > 60*time.Second {
		t.Fatalf("cancel tak menghentikan run blocking: %v", elapsed)
	}
	m, _ := res.(execResult)
	if m.Success {
		t.Fatalf("run yang dibatalkan harus gagal: %+v", res)
	}
	if m.TimedOut {
		t.Fatalf("cancel user bukan timeout: %+v", res)
	}
}

// Sesi background hidup lintas request (dipoll/dikill belakangan), jadi
// cancel request (/stop) tidak boleh membunuhnya — matikan via kill.
func TestRunExecBackgroundSurvivesParentCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, _ := runExec(ctx, t.TempDir(), sleepCmd(), 120, 64, true)
	m, _ := out.(map[string]any)
	sid, _ := m["sessionId"].(string)
	if m["success"] != true || sid == "" {
		t.Fatalf("background dengan parent cancel harus tetap jalan: %v", out)
	}
	defer killExec(sid)
	p, _ := pollExec(sid)
	pm, _ := p.(map[string]any)
	if pm["status"] != "running" {
		t.Fatalf("background harus tetap running walau parent cancel: %v", p)
	}
	if _, err := killExec(sid); err != nil {
		t.Fatal(err)
	}
}
