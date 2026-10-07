//go:build linux

package ai

import (
	"context"
	"os/exec"
	"testing"
)

func TestParseProcStat(t *testing.T) {
	// comm with spaces + parens must still parse.
	// RSS is the 24th field (index 21 after comm).
	// We put 1000 at index 21 (the RSS position).
	raw := "12345 (my prog (x)) S 1 777 777 0 -1 0 0 0 0 0 0 0 0 20 0 1 0 12345 1000 50 1000 0 0"
	g, rss, ok := parseProcStat([]byte(raw))
	if !ok || g != 777 {
		t.Fatalf("wrong pgrp: %d %v", g, ok)
	}
	if rss <= 0 {
		t.Fatalf("rss must be positive: %d", rss)
	}
	if _, _, ok := parseProcStat([]byte("sampah")); ok {
		t.Fatalf("corrupt input must fail")
	}
}

// A process group over the RAM budget must be killed (needs python3).
func TestMemBudgetKillsHog(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("butuh python3")
	}
	resAny, _ := runExec(context.Background(), t.TempDir(), `python3 -c "import time; a=bytearray(300_000_000); time.sleep(30)"`, 60, 64, false)
	res, _ := resAny.(execResult)
	if !res.MemoryLimited || res.Success {
		t.Fatalf("300MB hog over 64MB budget should be killed: %+v", res)
	}
}
