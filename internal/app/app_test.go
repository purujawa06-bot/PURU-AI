package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmc/langchaingo/llms"

	"github.com/purujawa06-bot/PURU-AI/internal/ai"
	"github.com/purujawa06-bot/PURU-AI/internal/config"
	"github.com/purujawa06-bot/PURU-AI/internal/history"
	"github.com/purujawa06-bot/PURU-AI/internal/memory"
	"github.com/purujawa06-bot/PURU-AI/internal/messages"
	"github.com/purujawa06-bot/PURU-AI/internal/telegram"
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

func userTextMsg(s string) *messages.Message {
	m := &messages.Message{Role: "user"}
	messages.SetContentString(m, s)
	return m
}

type stubSummarizer struct{ summary string }

func (s *stubSummarizer) GenerateContent(ctx context.Context, msgs []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: s.summary}}}, nil
}

func (s *stubSummarizer) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return s.summary, nil
}

type errSummarizer struct{}

func (errSummarizer) GenerateContent(ctx context.Context, msgs []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	return nil, errors.New("boom")
}

func (errSummarizer) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return "", errors.New("boom")
}

func TestMaybeCompactSummarizesWipesInjects(t *testing.T) {
	ws := t.TempDir()
	cfg := &config.Config{Workspace: ws, HistoryTokenLimit: 1} // paksa trigger
	hist := history.New(t.TempDir())
	mem := memory.New(ws)
	mem.Model = &stubSummarizer{summary: "## Done\n- topik A"}
	a := New(cfg, nil, hist, &ai.Agent{Client: mem.Model, Config: cfg}, mem)

	assistant := &messages.Message{Role: "assistant"}
	messages.SetContentString(assistant, "jawaban lama")
	stored := []*messages.Message{
		userTextMsg("lama, harus ikut diringkas"),
		assistant,
		userTextMsg("halo, bahas topik A yang panjang"),
	}
	got := a.maybeCompact(context.Background(), 42, stored)
	// wipe total: history kosong; ringkasan mengalir ke system prompt.
	if len(got) != 0 {
		t.Fatalf("harus wipe total, got %+v", got)
	}
	if saved := hist.Get(42); len(saved) != 0 {
		t.Fatalf("history harus kosong, got %+v", saved)
	}
	sys := a.renderedSystem()
	if !strings.Contains(sys, "## Done") {
		t.Fatalf("ringkasan terbaru harus di-inject ke system prompt, got %q", sys)
	}
	if _, err := os.Stat(filepath.Join(ws, "MEMORY.md")); !os.IsNotExist(err) {
		t.Errorf("MEMORY.md tidak boleh disentuh compact")
	}
}

func TestMaybeCompactFailsClosed(t *testing.T) {
	ws := t.TempDir()
	cfg := &config.Config{Workspace: ws, HistoryTokenLimit: 1}
	hist := history.New(t.TempDir())
	mem := memory.New(ws)
	mem.Model = errSummarizer{}
	a := New(cfg, nil, hist, &ai.Agent{Client: mem.Model, Config: cfg}, mem)
	stored := []*messages.Message{userTextMsg("hai yang panjang")}
	if got := a.maybeCompact(context.Background(), 1, stored); len(got) != 1 || got[0] != stored[0] {
		t.Fatalf("gagal summarize harus pertahankan history")
	}
}

func TestMaybeCompactBelowLimit(t *testing.T) {
	ws := t.TempDir()
	cfg := &config.Config{Workspace: ws, HistoryTokenLimit: 30000}
	hist := history.New(t.TempDir())
	a := New(cfg, nil, hist, nil, memory.New(ws))
	stored := []*messages.Message{userTextMsg("hai")}
	if got := a.maybeCompact(context.Background(), 1, stored); len(got) != 1 || got[0] != stored[0] {
		t.Fatalf("di bawah limit harus dikembalikan utuh")
	}
}

func TestHandleBlocksUnauthorized(t *testing.T) {
	ws := t.TempDir()
	cfg := &config.Config{Workspace: ws, TelegramAllowedUsers: []int64{111}}
	hist := history.New(t.TempDir())
	a := New(cfg, nil, hist, nil, memory.New(ws))
	upd := &telegram.Update{Message: &telegram.Message{
		MessageID: 1,
		From:      &telegram.User{ID: 999},
		Chat:      &telegram.Chat{ID: 999},
		Text:      "halo",
	}}
	if err := a.Handle(context.Background(), upd); err != nil {
		t.Fatalf("Handle unauthorized harus nil, got %v", err)
	}
	if got := hist.Get(999); len(got) != 0 {
		t.Fatalf("unauthorized tidak boleh menulis history, got %+v", got)
	}
}
