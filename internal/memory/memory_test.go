package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tmc/langchaingo/llms"

	"github.com/purujawa06-bot/PURU-AI/internal/messages"
)

func textMsg(role, s string) *messages.Message {
	m := &messages.Message{Role: role}
	messages.SetContentString(m, s)
	return m
}

type stubModel struct {
	summary string
	err     error
	prompt  string
}

func (s *stubModel) GenerateContent(ctx context.Context, msgs []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	for _, m := range msgs {
		for _, p := range m.Parts {
			if t, ok := p.(llms.TextContent); ok {
				s.prompt += t.Text
			}
		}
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: s.summary}}}, nil
}

func (s *stubModel) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return s.summary, s.err
}

func TestCompactSummarizesToMD(t *testing.T) {
	ws := t.TempDir()
	m := New(ws)
	m.Model = &stubModel{summary: "## Done\n- bahas topik A"}
	stored := []*messages.Message{textMsg("user", "halo"), textMsg("assistant", "hai")}
	rel, err := m.Compact(context.Background(), stored)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rel, "memory/context/") || !strings.HasSuffix(rel, ".md") {
		t.Fatalf("rel = %q", rel)
	}
	b, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "## Done") {
		t.Fatalf("file harus ringkasan model: %q", b)
	}
	// Ringkasan terbaru tersedia untuk inject ke system prompt.
	if got := m.Latest(); !strings.Contains(got, "## Done") {
		t.Fatalf("Latest = %q", got)
	}
	// Kosong → Latest "".
	empty := New(t.TempDir())
	if got := empty.Latest(); got != "" {
		t.Fatalf("Latest kosong = %q", got)
	}
	// MEMORY.md tidak boleh disentuh compactor.
	if _, err := os.Stat(filepath.Join(ws, "memory", "MEMORY.md")); !os.IsNotExist(err) {
		t.Errorf("MEMORY.md tidak boleh ditulis compactor")
	}
}

func TestCompactFailsClosedWithoutModel(t *testing.T) {
	ws := t.TempDir()
	m := New(ws) // Model nil
	if _, err := m.Compact(context.Background(), []*messages.Message{textMsg("user", "x")}); err == nil {
		t.Fatalf("tanpa model harus error (history dipertahankan)")
	}
	if entries, _ := os.ReadDir(m.ContextDir()); len(entries) != 0 {
		t.Fatalf("tanpa model tidak boleh menulis file")
	}
}

func TestCompactEmptyHistory(t *testing.T) {
	ws := t.TempDir()
	m := New(ws)
	m.Model = &stubModel{summary: "x"}
	if rel, err := m.Compact(context.Background(), nil); err != nil || rel != "" {
		t.Fatalf("rel=%q err=%v", rel, err)
	}
}

func TestCompactPrunesTo20(t *testing.T) {
	ws := t.TempDir()
	m := New(ws)
	m.Model = &stubModel{summary: "baru"}
	dir := m.ContextDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	for i := 0; i < 25; i++ {
		p := filepath.Join(dir, "2026-01-01_lama-"+strings.Repeat("a", i%5)+"-"+string(rune('a'+i%26))+".md")
		if err := os.WriteFile(p, []byte("# lama"), 0o644); err != nil {
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
	m.Model = &stubModel{summary: "x"}
	rel, err := m.Compact(context.Background(), []*messages.Message{textMsg("user", "halo")})
	if err != nil {
		t.Fatal(err)
	}
	name := strings.TrimPrefix(rel, "memory/context/")
	ok, err := filepath.Match("????-??-??_??-??-??.md", name)
	if err != nil || !ok {
		t.Fatalf("nama file harus tanggal+jam saja (YYYY-MM-DD_HH-MM-SS.md), got %q", rel)
	}
}
