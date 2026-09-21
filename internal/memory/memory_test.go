package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tmc/langchaingo/llms"
)

type stubModel struct{ content string }

func (s *stubModel) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: s.content}}}, nil
}

func (s *stubModel) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return s.content, nil
}

func TestCompactWritesContextFile(t *testing.T) {
	ws := t.TempDir()
	m := New(&stubModel{content: "TITLE: Belajar Go\n\n- user belajar Go\n- todo: latihan slice"}, ws)
	rel, err := m.Compact(context.Background(), "user: halo\nassistant: hai")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rel, "context/") || !strings.HasSuffix(rel, ".md") {
		t.Fatalf("rel = %q", rel)
	}
	b, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Belajar Go") || !strings.Contains(string(b), "latihan slice") {
		t.Fatalf("content = %q", b)
	}
	// MEMORY.md tidak boleh disentuh compactor.
	if _, err := os.Stat(filepath.Join(ws, "MEMORY.md")); !os.IsNotExist(err) {
		t.Errorf("MEMORY.md tidak boleh ditulis compactor")
	}
}

func TestCompactEmptyHistory(t *testing.T) {
	ws := t.TempDir()
	m := New(&stubModel{content: "TITLE: x\n\n- y"}, ws)
	if rel, err := m.Compact(context.Background(), "  "); err != nil || rel != "" {
		t.Fatalf("rel=%q err=%v", rel, err)
	}
}

func TestCompactPrunesTo20(t *testing.T) {
	ws := t.TempDir()
	m := New(&stubModel{content: "TITLE: Baru\n\n- baru"}, ws)
	dir := m.ContextDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	for i := 0; i < 25; i++ {
		p := filepath.Join(dir, "2026-01-01_lama-"+strings.Repeat("a", i%5)+"-"+string(rune('a'+i%26))+".md")
		if err := os.WriteFile(p, []byte("lama"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	rel, err := m.Compact(context.Background(), "user: x\nassistant: y")
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

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Belajar Go Dasar!":       "belajar-go-dasar",
		"  ":                      "ringkasan",
		"Fix bug #123 (urgent)":   "fix-bug-123-urgent",
		"Rencana liburan ke Bali": "rencana-liburan-ke-bali",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
