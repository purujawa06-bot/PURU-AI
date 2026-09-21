// Package app: slim Telegram handler for the lightweight local assistant.
// Text only. No uploads, no vision, no scheduler, no web, no usage tracking.
package app

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/purujawa06-bot/PURU-AI/internal/ai"
	"github.com/purujawa06-bot/PURU-AI/internal/config"
	"github.com/purujawa06-bot/PURU-AI/internal/history"
	"github.com/purujawa06-bot/PURU-AI/internal/memory"
	"github.com/purujawa06-bot/PURU-AI/internal/messages"
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
	return strings.HasPrefix(t, "/start") || strings.HasPrefix(t, "/menu") ||
		strings.HasPrefix(t, "/clear") || strings.HasPrefix(t, "/reset") ||
		strings.HasPrefix(t, "/help")
}

func (a *App) handleCommand(ctx context.Context, msg *telegram.Message) error {
	t := strings.TrimSpace(msg.Text)
	switch {
	case strings.HasPrefix(t, "/start"), strings.HasPrefix(t, "/menu"), strings.HasPrefix(t, "/help"):
		return a.safeReply(ctx, msg, "PURU-AI lightweight — kirim pesan apa saja. /clear = hapus history.", true)
	default: // /clear, /reset
		_ = a.hist.Clear(msg.From.ID)
		return a.safeReply(ctx, msg, "History dihapus.", true)
	}
}

// maybeCompact checks the 30k token trigger BEFORE the new prompt: when hit,
// summarize old memory + full history into MEMORY.md, then wipe history clean.
func (a *App) maybeCompact(ctx context.Context, userID int64, stored []*messages.Message) []*messages.Message {
	limit := a.cfg.HistoryTokenLimit
	if limit <= 0 || a.mem == nil || len(stored) == 0 {
		return stored
	}
	if history.TokenCount(stored) < limit {
		return stored
	}
	oldMem := a.mem.Read()
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if _, err := a.mem.Compact(cctx, oldMem, historyToText(stored)); err != nil {
		log.Printf("[memory] compact failed: %v", err)
		return stored
	}
	if err := a.hist.Clear(userID); err != nil {
		log.Printf("[memory] clear history failed: %v", err)
	}
	log.Printf("[memory] compacted + history wiped for user %d", userID)
	return nil
}

func historyToText(msgs []*messages.Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		if m == nil {
			continue
		}
		text := m.Text()
		if len(text) > 2000 {
			text = text[:2000]
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		sb.WriteString(text)
		sb.WriteString("\n")
	}
	return sb.String()
}

func (a *App) processMessage(ctx context.Context, msg *telegram.Message, userMessage string) error {
	userID := msg.From.ID
	stored := a.hist.Get(userID)
	// No pruning/capping — history grows until the 30k compaction trigger.
	stored = a.maybeCompact(ctx, userID, stored)

	thID, err := a.sendThinking(ctx, msg)
	if err != nil {
		return err
	}

	res := a.agent.ProcessMessage(ctx, userMessage, stored, &ai.ProcessOptions{ChatID: userID})

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
