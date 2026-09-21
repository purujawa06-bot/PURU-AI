package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/purujawa06-bot/PURU-AI/internal/messages"
)

func textMsg(role, s string) *messages.Message {
	m := &messages.Message{Role: role}
	messages.SetContentString(m, s)
	return m
}

func TestCompactDumpsRawJSON(t *testing.T) {
	ws := t.TempDir()
	m := New(ws)
	stored := []*messages.Message{textMsg("user", "halo"), textMsg("assistant", "hai")}
	rel, err := m.Compact(context.Background(), stored)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rel, "context/") || !strings.HasSuffix(rel, ".json") {
		t.Fatalf("rel = %q", rel)
	}
	b, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("file harus JSON mentah: %v (%q)", err, b)
	}
	if len(got) != 2 {
		t.Fatalf("harus 2 pesan, got %d", len(got))
	}
	// MEMORY.md tidak boleh disentuh compactor.
	if _, err := os.Stat(filepath.Join(ws, "MEMORY.md")); !os.IsNotExist(err) {
		t.Errorf("MEMORY.md tidak boleh ditulis compactor")
	}
}

func TestCompactEmptyHistory(t *testing.T) {
	ws := t.TempDir()
	m := New(ws)
	if rel, err := m.Compact(context.Background(), nil); err != nil || rel != "" {
		t.Fatalf("rel=%q err=%v", rel, err)
	}
}

func TestCompactPrunesTo20(t *testing.T) {
	ws := t.TempDir()
	m := New(ws)
	dir := m.ContextDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	for i := 0; i < 25; i++ {
		p := filepath.Join(dir, "2026-01-01_lama-"+strings.Repeat("a", i%5)+"-"+string(rune('a'+i%26))+".json")
		if err := os.WriteFile(p, []byte("[]"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	rel, err := m.Compact(context.Background(), []*messages.Message{textMsg("user", "x")})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != MaxSummaries {
		t.Fatalf("files = %d, want %d", len(entries), MaxSummaries)
	}
	if _, err := os.Stat(filepath.Join(ws, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("file baru harus dipertahankan: %v", err)
	}
}

func TestContextFilenameIsDateTimeOnly(t *testing.T) {
	ws := t.TempDir()
	m := New(ws)
	rel, err := m.Compact(context.Background(), []*messages.Message{textMsg("user", "halo")})
	if err != nil {
		t.Fatal(err)
	}
	name := strings.TrimPrefix(rel, "context/")
	ok, err := filepath.Match("????-??-??_??-??-??.json", name)
	if err != nil || !ok {
		t.Fatalf("nama file harus tanggal+jam saja (YYYY-MM-DD_HH-MM-SS.json), got %q", rel)
	}
}
