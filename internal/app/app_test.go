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

func TestIsCommandMenu(t *testing.T) {
	for _, c := range []string{"/help", "/help@bot", "/clear", "/token", "/stop", "/stop@bot"} {
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

// /stop tanpa proses berjalan harus false (tak ada yang dihentikan).
func TestStopUserIdle(t *testing.T) {
	ws := t.TempDir()
	a := New(&config.Config{Workspace: ws}, nil, history.New(t.TempDir()), nil, memory.New(ws))
	if a.stopUser(7) {
		t.Fatalf("stopUser idle harus false")
	}
}

// /stop harus memanggil cancel sesi dan melepas busy-guard; sesi baru yang
// mulai setelahnya tidak boleh ikut terlepas oleh goroutine lama.
func TestStopUserCancelsAndReleases(t *testing.T) {
	ws := t.TempDir()
	a := New(&config.Config{Workspace: ws}, nil, history.New(t.TempDir()), nil, memory.New(ws))
	_, cancel := context.WithCancel(context.Background())
	old := &busySession{cancel: cancel}
	if !a.tryAcquire(7, old) {
		t.Fatalf("tryAcquire harus berhasil")
	}
	if !a.stopUser(7) {
		t.Fatalf("stopUser harus true saat ada sesi")
	}
	if _, ok := a.busy.Load(int64(7)); ok {
		t.Fatalf("busy harus dilepas setelah /stop")
	}
	// Sesi baru mulai; goroutine lama yang selesai belakangan tak boleh
	// menendang entry baru (releaseCancel hanya hapus pointer miliknya).
	_, cancel2 := context.WithCancel(context.Background())
	cur := &busySession{cancel: cancel2}
	if !a.tryAcquire(7, cur) {
		t.Fatalf("tryAcquire sesi baru harus berhasil")
	}
	a.releaseCancel(7, old)
	if v, ok := a.busy.Load(int64(7)); !ok || v != any(cur) {
		t.Fatalf("releaseCancel sesi lama tak boleh hapus sesi baru")
	}
	a.releaseCancel(7, cur)
	if _, ok := a.busy.Load(int64(7)); ok {
		t.Fatalf("releaseCancel sesi sendiri harus melepas")
	}
	if a.stopUser(7) {
		t.Fatalf("stopUser kedua harus false")
	}
}

// Ronde 21: grup diam kecuali /ai. isCommand tetap 4 command instan.
func TestIsCommandIgnoresAI(t *testing.T) {
	for _, c := range []string{"/ai halo", "/ai@bot halo", "/ai"} {
		if isCommand(c) {
			t.Errorf("%q bukan command instan, harus false", c)
		}
	}
}

func TestIsGroupChat(t *testing.T) {
	if !isGroupChat("group") || !isGroupChat("supergroup") {
		t.Fatalf("group/supergroup harus true")
	}
	for _, c := range []string{"private", "channel", ""} {
		if isGroupChat(c) {
			t.Errorf("%q harus false", c)
		}
	}
}

func TestParseAICommand(t *testing.T) {
	cases := map[string]struct {
		ok   bool
		rest string
	}{
		"/ai halo":        {true, "halo"},
		"/ai  halo dunia": {true, "halo dunia"},
		"/ai@bot halo":    {true, "halo"},
		"/ai@bot":         {true, ""},
		"/ai":             {true, ""},
		"/aid bukan":      {false, ""},
		"/aix":            {false, ""},
		"halo /ai":        {false, ""},
		"halo":            {false, ""},
	}
	for in, want := range cases {
		rest, ok := parseAICommand(in)
		if ok != want.ok || rest != want.rest {
			t.Errorf("parseAICommand(%q) = (%q,%v), want (%q,%v)", in, rest, ok, want.rest, want.ok)
		}
	}
}
