// Package app: slim Telegram handler for the lightweight local assistant.
// Text only. No uploads, no vision, no web, no usage tracking.
// Includes Picoclaw cron-like scheduled tasks via the schedule tool + runner.
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

// Status texts for the shared thinking message lifecycle:
// "thinking" -> "compacting" (during summarize) -> "thinking" (agent run).
const (
	thinkingText   = "🤔 ..."
	compactingText = "🗜️ Compacting memory..."
)

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

// busySession is a per-user busy-guard entry. Stored as a pointer so
// CompareAndDelete is safe: func values (context.CancelFunc) cannot be
// compared with == (panic), so the pointer is the comparable key.
type busySession struct {
	cancel context.CancelFunc
}

func (a *App) tryAcquire(userID int64, sess *busySession) bool {
	if sess == nil || sess.cancel == nil {
		_, loaded := a.busy.LoadOrStore(userID, struct{}{})
		return !loaded
	}
	_, loaded := a.busy.LoadOrStore(userID, sess)
	return !loaded
}

// releaseCancel releases only when the entry still belongs to this session
// (same sess pointer) — a late-finishing goroutine must not evict a newer
// session started after /stop.
func (a *App) releaseCancel(userID int64, sess *busySession) {
	if sess == nil {
		a.busy.Delete(userID)
		return
	}
	a.busy.CompareAndDelete(userID, sess)
}

// stopUser cancels the running agent process for that user.
// Returns true when a process was stopped, false when none.
// Uses CompareAndDelete on the Loaded value (pointer, comparable) to avoid
// deleting a new session started right after Load (/stop vs new message race).
// Safe from func-compare panic because pointers are compared.
func (a *App) stopUser(userID int64) bool {
	v, ok := a.busy.Load(userID)
	if !ok {
		return false
	}
	if sess, ok := v.(*busySession); ok && sess != nil && sess.cancel != nil {
		sess.cancel()
	}
	a.busy.CompareAndDelete(userID, v)
	return true
}

// Handle dispatches one update async per user (busy-guarded).
// In groups (group/supergroup) the bot stays silent unless called via /ai or
// instant commands (/help /clear /token /stop). In private all text is processed.
func (a *App) Handle(ctx context.Context, upd *telegram.Update) error {
	if upd.Message == nil || upd.Message.From == nil || upd.Message.Chat == nil {
		return nil
	}
	msg := upd.Message
	userID := msg.From.ID

	// 1. Filter: In groups, only respond to /ai or bot commands to avoid spamming unauthorized users.
	userMessage := msg.Text
	if isGroupChat(msg.Chat.Type) {
		if !isCommand(msg.Text) {
			rest, ok := parseAICommand(msg.Text)
			if !ok {
				return nil
			}
			userMessage = strings.TrimSpace(rest)
		}
	}

	// 2. Check permission
	if a.cfg != nil && !a.cfg.IsUserAllowed(userID) {
		log.Printf("[app] blocked unauthorized user %d", userID)
		if a.tg == nil {
			return nil
		}
		return a.safeReply(ctx, msg, "⛔ Sorry, you are not registered to use this bot.", true)
	}

	if strings.TrimSpace(msg.Text) == "" {
		return nil
	}

	// 3. Proceed with command or AI
	if isGroupChat(msg.Chat.Type) {
		if isCommand(msg.Text) {
			go func() {
				if err := a.handleCommand(ctx, msg); err != nil {
					log.Printf("[app] command user %d: %v", userID, err)
				}
			}()
			return nil
		}
		rest, ok := parseAICommand(msg.Text)
		if !ok {
			return nil
		}
		if strings.TrimSpace(rest) == "" {
			if a.tg == nil {
				return nil
			}
			return a.safeReply(ctx, msg, "Send /ai <question>.", true)
		}
		userMessage = strings.TrimSpace(rest)
	} else if isCommand(msg.Text) {
		go func() {
			if err := a.handleCommand(ctx, msg); err != nil {
				log.Printf("[app] command user %d: %v", userID, err)
			}
		}()
		return nil
	}
	rctx, cancel := context.WithCancel(ctx)
	sess := &busySession{cancel: cancel}
	if !a.tryAcquire(userID, sess) {
		cancel()
		return a.safeReply(ctx, msg, "⏳ Still processing your previous message, please wait a moment...", true)
	}
	go func() {
		defer cancel()
		defer a.releaseCancel(userID, sess)
		if err := a.processMessage(rctx, msg, userMessage); err != nil {
			log.Printf("[app] handle user %d: %v", userID, err)
		}
	}()
	return nil
}

// isGroupChat reports Telegram group chats (group/supergroup).
func isGroupChat(t string) bool {
	return t == "group" || t == "supergroup"
}

// parseAICommand matches /ai and /ai@botname at the start of the text.
// Returns the remainder + true on match; /aid and friends are not /ai.
func parseAICommand(s string) (string, bool) {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "/ai") {
		return "", false
	}
	rest := strings.TrimPrefix(t, "/ai")
	if rest == "" || strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\t") || strings.HasPrefix(rest, "\n") {
		return strings.TrimSpace(rest), true
	}
	if strings.HasPrefix(rest, "@") {
		if i := strings.IndexAny(rest, " \t\n"); i >= 0 {
			return strings.TrimSpace(rest[i:]), true
		}
		return "", true
	}
	return "", false
}

func isCommand(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "/help") ||
		strings.HasPrefix(t, "/clear") ||
		strings.HasPrefix(t, "/token") ||
		strings.HasPrefix(t, "/stop") ||
		strings.HasPrefix(t, "/sched")
}

func (a *App) handleCommand(ctx context.Context, msg *telegram.Message) error {
	t := strings.TrimSpace(msg.Text)
	switch {
	case strings.HasPrefix(t, "/stop"):
		if a.stopUser(msg.From.ID) {
			return a.safeReply(ctx, msg, "⏹️ Process stopped.", true)
		}
		return a.safeReply(ctx, msg, "No process is running.", true)
	case strings.HasPrefix(t, "/token"):
		return a.safeReply(ctx, msg, tokenInfo(history.TokenCountFull(a.renderedSystemFor(msg.From.ID), a.hist.Get(msg.From.ID)), a.cfg.HistoryTokenLimit), true)
	case strings.HasPrefix(t, "/help"):
		return a.safeReply(ctx, msg, "PURU-AI lightweight — just send any message.\n/clear = clear history.\n/token = memory token usage info.\n/stop = stop the running process.\n/sched = list scheduled jobs (ask me to schedule, e.g. \"every day 6am WIB check stocks\").\nIn groups: call via /ai <question> (e.g. /ai explain Raft).", true)
	case strings.HasPrefix(t, "/sched"):
		return a.handleSchedCommand(ctx, msg)
	default: // /clear
		_ = a.hist.Clear(msg.From.ID)
		return a.safeReply(ctx, msg, "History cleared.", true)
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
		" (" + fmtPct(pct) + ")\nSummarized + history cleared at 100% (" + fmtInt(left) + " left)."
}

// fmtInt formats n with ',' thousands separator: 30000 -> "30,000".
func fmtInt(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// fmtPct formats a percent with one decimal: 4.06 -> "4.1%".
func fmtPct(p float64) string {
	return strconv.FormatFloat(p, 'f', 1, 64) + "%"
}

// renderedSystem renders the same system prompt the agent sends on every
// request (template + memory/MEMORY.md + latest memory/context/*.md summary)
// so token counting matches reality.
func (a *App) renderedSystem() string {
	return a.renderedSystemFor(0)
}

// renderedSystemFor renders the prompt exactly like a real request for the
// given chat: it adds that chat's runtime-active skills so the token count
// includes the injected skill bodies.
func (a *App) renderedSystemFor(chatID int64) string {
	mem := ""
	summary := ""
	workspace := ""
	if a.cfg != nil {
		if b, err := os.ReadFile(a.cfg.MemoryPath()); err == nil {
			mem = string(b)
		}
		summary = memory.LatestSummary(a.cfg.Workspace)
		workspace = a.cfg.Workspace
	}
	var opts *ai.ProcessOptions
	if chatID != 0 && a.agent != nil {
		opts = &ai.ProcessOptions{ChatID: chatID}
	}
	s, err := prompt.Build(prompt.Request{
		Workspace:    workspace,
		Memory:       mem,
		Summary:      summary,
		ActiveSkills: ai.ActiveSkillsFor(a.agent, opts),
		Policy:       a.cfg.SkillsPolicy(),
	})
	if err != nil {
		return ""
	}
	return s
}

// compactNeeded reports whether stored history hits the summarize trigger.
// Single token-count check shared by maybeCompact and the feedback variant.
func (a *App) compactNeeded(stored []*messages.Message) bool {
	return a.compactNeededFor(0, stored)
}

// compactNeededFor is the per-chat variant: the token count includes that
// chat's runtime-active skills so compaction triggers on the real size.
func (a *App) compactNeededFor(chatID int64, stored []*messages.Message) bool {
	limit := 0
	if a.cfg != nil {
		limit = a.cfg.HistoryTokenLimit
	}
	if limit <= 0 || a.mem == nil || len(stored) == 0 {
		return false
	}
	return history.TokenCountFull(a.renderedSystemFor(chatID), stored) >= limit
}

// runCompact summarizes history into memory/context, wipes history, and
// returns the kept messages. On failure history is kept as-is for retry.
func (a *App) runCompact(ctx context.Context, userID int64, stored []*messages.Message) []*messages.Message {
	if a.mem.Model == nil && a.agent != nil {
		a.mem.Model = a.agent.Client
	}
	rel, err := a.mem.Compact(ctx, stored)
	if err != nil {
		log.Printf("[memory] compact failed: %v", err)
		return stored
	}
	if rel == "" {
		return stored
	}
	kept := []*messages.Message{}
	if err := a.hist.Set(userID, kept); err != nil {
		log.Printf("[memory] save note failed: %v", err)
	}
	log.Printf("[memory] compacted -> %s for user %d", rel, userID)
	return kept
}

// editThinking updates the shared status message, ignoring nil Telegram.
func (a *App) editThinking(ctx context.Context, chatID, msgID int64, text string) {
	if a.tg == nil || chatID == 0 || msgID == 0 {
		return
	}
	if err := a.tg.EditMessage(ctx, chatID, msgID, text); err != nil {
		log.Printf("[app] thinking edit: %v", err)
	}
}

// maybeCompact checks the token trigger BEFORE the new prompt: when hit,
// the model summarizes full history into memory/context/YYYY-MM-DD_HH-MM-SS.md,
// history is wiped, and the new summary flows into the system prompt on the
// next request (see memory.LatestSummary). On summarize failure history is
// kept as-is and the next message retries.
func (a *App) maybeCompact(ctx context.Context, userID int64, stored []*messages.Message) []*messages.Message {
	if !a.compactNeededFor(userID, stored) {
		return stored
	}
	return a.runCompact(ctx, userID, stored)
}

// maybeCompactWithFeedback wraps runCompact with Telegram status updates on
// the shared thinking message: it shows compactingText while the summarize
// model call runs, then edits the same message back to thinkingText when
// compaction finishes (success or failure) so the agent run continues from
// the loading state. When no compaction triggers, stored is returned
// untouched without any Telegram edit.
func (a *App) maybeCompactWithFeedback(ctx context.Context, userID int64, stored []*messages.Message, chatID, thinkingID int64) []*messages.Message {
	if !a.compactNeededFor(userID, stored) {
		return stored
	}
	a.editThinking(ctx, chatID, thinkingID, compactingText)
	kept := a.runCompact(ctx, userID, stored)
	a.editThinking(ctx, chatID, thinkingID, thinkingText)
	return kept
}

func (a *App) processMessage(ctx context.Context, msg *telegram.Message, userMessage string) error {
	userID := msg.From.ID
	stored := a.hist.Get(userID)

	thID, err := a.sendThinking(ctx, msg)
	if err != nil {
		return err
	}

	// No pruning/capping — history grows until the compaction trigger.
	// The same thinking message shows compacting progress, then flips back
	// to the loading state when summarize finishes.
	stored = a.maybeCompactWithFeedback(ctx, userID, stored, msg.Chat.ID, thID)

	opts := &ai.ProcessOptions{ChatID: userID, Channel: "telegram"}
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
	if ctx.Err() != nil {
		if a.tg != nil {
			_ = a.tg.DeleteMessage(context.Background(), msg.Chat.ID, thID)
		}
		return ctx.Err()
	}

	saved := make([]*messages.Message, 0, len(stored)+1+len(res.ResponseMessages))
	saved = append(saved, stored...)
	u := &messages.Message{Role: "user"}
	messages.SetContentString(u, userMessage)
	saved = append(saved, u)
	// Keep as-is (reasoning + empty responses preserved);
	// only size-truncated via Sanitize. No prune — compaction handles
	// it at history_token_limit.
	saved = append(saved, messages.SanitizeHistoryMessages(res.ResponseMessages)...)
	_ = a.hist.Set(userID, saved)

	if err := a.safeSend(ctx, msg, res.Text); err != nil {
		log.Printf("[app] send reply failed: %v", err)
	}
	if a.tg != nil {
		_ = a.tg.DeleteMessage(ctx, msg.Chat.ID, thID)
	}
	return nil
}

func (a *App) sendThinking(ctx context.Context, msg *telegram.Message) (int64, error) {
	if a.tg == nil {
		return 0, errors.New("telegram client not configured")
	}
	return a.tg.SendMessage(ctx, msg.Chat.ID, thinkingText, map[string]any{"reply_to_message_id": msg.MessageID})
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
		if a.tg == nil {
			return
		}
		if err := a.tg.EditMessage(ctx, chatID, msgID, strings.Join(lines, "\n")); err != nil {
			log.Printf("[app] preview edit: %v", err)
		}
	}
}

// toolArgPreview shows the most relevant arg for a tool call.
func toolArgPreview(name string, args map[string]any) string {
	switch name {
	case "read_file", "write_file", "list_dir", "edit_file_replace_string", "edit_file_replace_line", "edit_file_apply_patch", "append_file", "telegram_sendfile":
		return previewStr(args["path"])
	case "use_skill", "stop_skill":
		return previewStr(args["name"])
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
		_ = a.safeReply(ctx, msg, "⚠️ Response too long, sent as a file.", false)
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

// handleSchedCommand implements /sched: list jobs, remove one, or show help.
// Full scheduling is conversational via the schedule AI tool.
func (a *App) handleSchedCommand(ctx context.Context, msg *telegram.Message) error {
	text := strings.TrimSpace(msg.Text)
	fields := strings.Fields(text)
	if len(fields) >= 3 && (fields[1] == "remove" || fields[1] == "rm" || fields[1] == "del") {
		id := strings.TrimSpace(fields[2])
		store := a.ScheduleStore()
		if store == nil {
			return a.safeReply(ctx, msg, "Schedule unavailable: workspace not configured.", true)
		}
		if err := store.Remove(id); err != nil {
			return a.safeReply(ctx, msg, "Cannot remove job: "+err.Error(), true)
		}
		return a.safeReply(ctx, msg, "Removed job "+id+".", true)
	}
	store := a.ScheduleStore()
	if store == nil {
		return a.safeReply(ctx, msg, "Schedule unavailable: workspace not configured.", true)
	}
	chatID := msg.Chat.ID
	jobs, err := store.List(chatID)
	if err != nil {
		return a.safeReply(ctx, msg, "Cannot list jobs: "+err.Error(), true)
	}
	return a.safeReply(ctx, msg, FormatScheduleList(jobs), true)
}
