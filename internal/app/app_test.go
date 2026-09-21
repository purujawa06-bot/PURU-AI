package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmc/langchaingo/llms"

	"github.com/purujawa06-bot/PURU-AI/internal/config"
	"github.com/purujawa06-bot/PURU-AI/internal/history"
	"github.com/purujawa06-bot/PURU-AI/internal/memory"
	"github.com/purujawa06-bot/PURU-AI/internal/messages"
)

func TestIsCommandOnlyThree(t *testing.T) {
	for _, c := range []string{"/help", "/help@bot", "/clear", "/token"} {
		if !isCommand(c) {
			t.Errorf("%q harus dikenali sebagai command", c)
		}
	}
	for _, c := range []string{"/start", "/menu", "/reset", "halo"} {
		if isCommand(c) {
			t.Errorf("%q tidak boleh jadi command lagi", c)
		}
	}
}

func TestTokenInfoPercent(t *testing.T) {
	got := tokenInfo(15000, 30000)
	if !strings.Contains(got, "15.000 / 30.000") || !strings.Contains(got, "50,0%") {
		t.Fatalf("got %q", got)
	}
	got = tokenInfo(0, 30000)
	if !strings.Contains(got, "0 / 30.000") || !strings.Contains(got, "0,0%") {
		t.Fatalf("got %q", got)
	}
}

func TestFmtInt(t *testing.T) {
	cases := map[int]string{0: "0", 999: "999", 1000: "1.000", 30000: "30.000", 1234567: "1.234.567"}
	for in, want := range cases {
		if got := fmtInt(in); got != want {
			t.Errorf("fmtInt(%d) = %q, want %q", in, got, want)
		}
	}
}

type stubModel struct{ content string }

func (s *stubModel) GenerateContent(ctx context.Context, msgs []llms.MessageContent, opts ...llms.CallOption) (*llms.ContentResponse, error) {
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: s.content}}}, nil
}

func (s *stubModel) Call(ctx context.Context, prompt string, opts ...llms.CallOption) (string, error) {
	return s.content, nil
}

func userTextMsg(s string) *messages.Message {
	m := &messages.Message{Role: "user"}
	messages.SetContentString(m, s)
	return m
}

func TestMaybeCompactInjectsPathOnly(t *testing.T) {
	ws := t.TempDir()
	cfg := &config.Config{Workspace: ws, HistoryTokenLimit: 1} // paksa trigger
	hist := history.New(t.TempDir())
	mem := memory.New(&stubModel{content: "TITLE: Topik A\n\n- bahas A"}, ws)
	a := New(cfg, nil, hist, nil, mem)

	stored := []*messages.Message{userTextMsg("halo, bahas topik A yang panjang")}
	got := a.maybeCompact(context.Background(), 42, stored)
	if len(got) != 1 || got[0].Role != "system" {
		t.Fatalf("harus 1 system note, got %+v", got)
	}
	if !strings.Contains(got[0].Text(), "context/") {
		t.Fatalf("note harus berisi path saja, got %q", got[0].Text())
	}
	saved := hist.Get(42)
	if len(saved) != 1 || saved[0].Text() != got[0].Text() {
		t.Fatalf("history harus = note, got %+v", saved)
	}
	if _, err := os.Stat(filepath.Join(ws, "MEMORY.md")); !os.IsNotExist(err) {
		t.Errorf("MEMORY.md tidak boleh disentuh compact")
	}
}

func TestMaybeCompactBelowLimit(t *testing.T) {
	ws := t.TempDir()
	cfg := &config.Config{Workspace: ws, HistoryTokenLimit: 30000}
	hist := history.New(t.TempDir())
	a := New(cfg, nil, hist, nil, memory.New(&stubModel{}, ws))
	stored := []*messages.Message{userTextMsg("hai")}
	if got := a.maybeCompact(context.Background(), 1, stored); len(got) != 1 || got[0] != stored[0] {
		t.Fatalf("di bawah limit harus dikembalikan utuh")
	}
}
