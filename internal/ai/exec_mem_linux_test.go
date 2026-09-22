//go:build linux

package ai

import (
	"context"
	"os/exec"
	"testing"
)

func TestParseProcStat(t *testing.T) {
	// comm dengan spasi + kurung harus tetap ke-parse.
	// RSS adalah field ke-24 (index 21 setelah comm).
	// Kita taruh 1000 di index 21 (posisi RSS).
	raw := "12345 (my prog (x)) S 1 777 777 0 -1 0 0 0 0 0 0 0 0 20 0 1 0 12345 1000 50 1000 0 0"
	g, rss, ok := parseProcStat([]byte(raw))
	if !ok || g != 777 {
		t.Fatalf("pgrp salah: %d %v", g, ok)
	}
	if rss <= 0 {
		t.Fatalf("rss harus positif: %d", rss)
	}
	if _, _, ok := parseProcStat([]byte("sampah")); ok {
		t.Fatalf("input rusak harus gagal")
	}
}

// Grup proses yang lewat budget RAM harus di-kill (butuh python3).
func TestMemBudgetKillsHog(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("butuh python3")
	}
	resAny, _ := runExec(context.Background(), t.TempDir(), `python3 -c "import time; a=bytearray(300_000_000); time.sleep(30)"`, 60, 64, false)
	res, _ := resAny.(execResult)
	if !res.MemoryLimited || res.Success {
		t.Fatalf("hog 300MB dengan budget 64MB harus di-kill: %+v", res)
	}
}
