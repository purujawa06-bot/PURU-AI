// Package app: slim Telegram handler for the lightweight local assistant.
// Text only. No uploads, no vision, no scheduler, no web, no usage tracking.
package app

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/purujawa06-bot/PURU-AI/internal/ai"
	"github.com/purujawa06-bot/PURU-AI/internal/config"
	"github.com/purujawa06-bot/PURU-AI/internal/history"
	"github.com/purujawa06-bot/PURU-AI/internal/memory"
	"github.com/purujawa06-bot/PURU-AI/internal/messages"
	"github.com/purujawa06-bot/PURU-AI/internal/prompt"
	"github.com/purujawa06-bot/PURU-AI/internal/telegram"
)

const maxMessageLength = 4096

type App struct {
	cfg   *config.Config
	tg    *telegram.API
	hist  *history.Store
	agent *ai.Agent
	mem   *memory.Manager
	busy  sync.Map
}

func New(cfg *config.Config, tg *telegram.API, h *history.Store, a *ai.Agent, m *memory.Manager) *App {
	return &App{cfg: cfg, tg: tg, hist: h, agent: a, mem: m}
}

func (a *App) tryAcquire(userID int64) bool {
	_, loaded := a.busy.LoadOrStore(userID, struct{}{})
	return !loaded
}

func (a *App) release(userID int64) { a.busy.Delete(userID) }

// Handle dispatches one update async per user (busy-guarded).
func (a *App) Handle(ctx context.Context, upd *telegram.Update) error {
	if upd.Message == nil || upd.Message.From == nil || upd.Message.Chat == nil {
		return nil
	}
	msg := upd.Message
	if strings.TrimSpace(msg.Text) == "" {
		return nil
	}
	userID := msg.From.ID
	if isCommand(msg.Text) {
		go func() {
			if err := a.handleCommand(ctx, msg); err != nil {
				log.Printf("[app] command user %d: %v", userID, err)
			}
		}()
		return nil
	}
	if !a.tryAcquire(userID) {
		return a.safeReply(ctx, msg, "⏳ Masih ada yang diproses, tunggu sebentar ya...", true)
	}
	go func() {
		defer a.release(userID)
		if err := a.processMessage(ctx, msg, msg.Text); err != nil {
			log.Printf("[app] handle user %d: %v", userID, err)
		}
	}()
	return nil
}

func isCommand(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "/help") ||
		strings.HasPrefix(t, "/clear") ||
		strings.HasPrefix(t, "/token")
}

func (a *App) handleCommand(ctx context.Context, msg *telegram.Message) error {
	t := strings.TrimSpace(msg.Text)
	switch {
	case strings.HasPrefix(t, "/token"):
		return a.safeReply(ctx, msg, tokenInfo(history.TokenCountFull(a.renderedSystem(), a.hist.Get(msg.From.ID)), a.cfg.HistoryTokenLimit), true)
	case strings.HasPrefix(t, "/help"):
		return a.safeReply(ctx, msg, "PURU-AI lightweight — kirim pesan apa saja.\n/clear = hapus history.\n/token = info pemakaian token memory.", true)
	default: // /clear
		_ = a.hist.Clear(msg.From.ID)
		return a.safeReply(ctx, msg, "History dihapus.", true)
	}
}

// tokenInfo reports history usage vs the compaction limit: how full memory is
// before it gets summarized (100%) and wiped.
func tokenInfo(used, limit int) string {
	if limit <= 0 {
		limit = 30000
	}
	if used < 0 {
		used = 0
	}
	pct := float64(used) / float64(limit) * 100
	left := limit - used
	if left < 0 {
		left = 0
	}
	return "📊 Token memory: " + fmtInt(used) + " / " + fmtInt(limit) +
		" (" + fmtPct(pct) + ")\nSummarize + hapus history saat 100% (sisa " + fmtInt(left) + ")."
}

// fmtInt formats n with '.' thousands separator (id style): 30000 -> "30.000".
func fmtInt(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// fmtPct formats a percent with one comma decimal: 4.06 -> "4,1%".
func fmtPct(p float64) string {
	s := strconv.FormatFloat(p, 'f', 1, 64)
	return strings.Replace(s, ".", ",", 1) + "%"
}

// renderedSystem renders the same system prompt the agent sends on every
// request (template + MEMORY.md) so token counting matches reality.
func (a *App) renderedSystem() string {
	mem := ""
	if a.cfg != nil {
		if b, err := os.ReadFile(a.cfg.MemoryPath()); err == nil {
			mem = string(b)
		}
	}
	s, err := prompt.Get(mem)
	if err != nil {
		return ""
	}
	return s
}

// maybeCompact checks the token trigger BEFORE the new prompt: when hit,
// dump full history raw into context/YYYY-MM-DD_HH-MM-SS.json, wipe history,
// and inject back ONLY the dump path as a system note (content is never
// injected — the AI reads the file when it needs old context).
func (a *App) maybeCompact(ctx context.Context, userID int64, stored []*messages.Message) []*messages.Message {
	limit := a.cfg.HistoryTokenLimit
	if limit <= 0 || a.mem == nil || len(stored) == 0 {
		return stored
	}
	if history.TokenCountFull(a.renderedSystem(), stored) < limit {
		return stored
	}
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	rel, err := a.mem.Compact(cctx, stored)
	if err != nil {
		log.Printf("[memory] compact failed: %v", err)
		return stored
	}
	if rel == "" {
		return stored
	}
	note := &messages.Message{Role: "system"}
	messages.SetContentString(note, memory.SummaryNote(rel))
	kept := []*messages.Message{note}
	if err := a.hist.Set(userID, kept); err != nil {
		log.Printf("[memory] save note failed: %v", err)
	}
	log.Printf("[memory] compacted -> %s for user %d", rel, userID)
	return kept
}

func (a *App) processMessage(ctx context.Context, msg *telegram.Message, userMessage string) error {
	userID := msg.From.ID
	stored := a.hist.Get(userID)
	// No pruning/capping — history grows until the compaction trigger.
	stored = a.maybeCompact(ctx, userID, stored)

	thID, err := a.sendThinking(ctx, msg)
	if err != nil {
		return err
	}

	opts := &ai.ProcessOptions{ChatID: userID}
	if msg.From != nil {
		opts.User = &ai.TelegramUser{
			ID: msg.From.ID, Username: msg.From.Username,
			FirstName: msg.From.FirstName, LastName: msg.From.LastName,
		}
	}
	if a.cfg.ShowToolsPreview() {
		opts.OnTool = a.previewHook(ctx, msg.Chat.ID, thID)
	}

	res := a.agent.ProcessMessage(ctx, userMessage, stored, opts)

	saved := make([]*messages.Message, 0, len(stored)+1+len(res.ResponseMessages))
	saved = append(saved, stored...)
	u := &messages.Message{Role: "user"}
	messages.SetContentString(u, userMessage)
	saved = append(saved, u)
	saved = append(saved, messages.SanitizeHistoryMessages(res.ResponseMessages)...)
	_ = a.hist.Set(userID, saved)

	if err := a.safeSend(ctx, msg, res.Text); err != nil {
		log.Printf("[app] send reply failed: %v", err)
	}
	_ = a.tg.DeleteMessage(ctx, msg.Chat.ID, thID)
	return nil
}

func (a *App) sendThinking(ctx context.Context, msg *telegram.Message) (int64, error) {
	return a.tg.SendMessage(ctx, msg.Chat.ID, "🤔 ...", map[string]any{"reply_to_message_id": msg.MessageID})
}

// previewHook returns an OnTool callback that live-edits the thinking message
// with the tools being used (throttled: max ~1 edit per 1.2s).
func (a *App) previewHook(ctx context.Context, chatID, msgID int64) func(string, map[string]any) {
	var mu sync.Mutex
	lines := []string{}
	var last time.Time
	return func(name string, args map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, "🔧 "+name+" "+truncPreview(toolArgPreview(name, args)))
		if len(lines) > 6 {
			lines = lines[len(lines)-6:]
		}
		if time.Since(last) < 1200*time.Millisecond {
			return
		}
		last = time.Now()
		if err := a.tg.EditMessage(ctx, chatID, msgID, strings.Join(lines, "\n")); err != nil {
			log.Printf("[app] preview edit: %v", err)
		}
	}
}

// toolArgPreview shows the most relevant arg for a tool call.
func toolArgPreview(name string, args map[string]any) string {
	switch name {
	case "edit_file", "telegram_sendfile":
		return previewStr(args["path"])
	case "exec":
		return previewStr(args["command"])
	default:
		return ""
	}
}

func previewStr(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func truncPreview(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 60 {
		return s[:60] + "…"
	}
	return s
}

func (a *App) safeReply(ctx context.Context, msg *telegram.Message, text string, replyTo bool) error {
	opts := map[string]any{}
	if replyTo && msg.MessageID > 0 {
		opts["reply_to_message_id"] = msg.MessageID
	}
	return a.withMarkdownFallback(func(pm string) error {
		o := map[string]any{}
		for k, v := range opts {
			o[k] = v
		}
		if pm != "" {
			o["parse_mode"] = pm
		}
		_, err := a.tg.SendMessage(ctx, msg.Chat.ID, text, o)
		return err
	})
}

func (a *App) safeSend(ctx context.Context, msg *telegram.Message, text string) error {
	if len(text) > maxMessageLength {
		_ = a.safeReply(ctx, msg, "⚠️ Respon terlalu panjang, dikirim sebagai file.", false)
		return a.tg.SendFile(ctx, msg.Chat.ID, "respon.md", []byte(text), "Respon lengkap.")
	}
	return a.safeReply(ctx, msg, text, true)
}

func (a *App) withMarkdownFallback(fn func(parseMode string) error) error {
	if err := fn("Markdown"); err == nil {
		return nil
	} else {
		var te *telegram.TelegramError
		if errors.As(err, &te) && te.Code == 400 && strings.Contains(te.Message, "parse entities") {
			return fn("")
		}
		return err
	}
}
