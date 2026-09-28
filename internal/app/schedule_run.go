// Scheduled job execution as agent turns with Telegram delivery.
package app

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/purujawa06-bot/PURU-AI/internal/ai"
	"github.com/purujawa06-bot/PURU-AI/internal/messages"
	"github.com/purujawa06-bot/PURU-AI/internal/schedule"
)

// ScheduleStore returns the workspace-backed job store.
func (a *App) ScheduleStore() *schedule.Store {
	ws := ""
	if a != nil && a.cfg != nil {
		ws = a.cfg.Workspace
	}
	if strings.TrimSpace(ws) == "" {
		return nil
	}
	return schedule.NewStore(ws)
}

// RunScheduledJob executes one due job as an agent turn and delivers
// the result to the originating chat. History is loaded and saved so
// scheduled work shares context with normal conversation.
func (a *App) RunScheduledJob(ctx context.Context, job schedule.Job) error {
	if a == nil || a.agent == nil {
		return fmt.Errorf("agent not configured")
	}
	if job.ChatID == 0 {
		return fmt.Errorf("job %s has no chat id", job.ID)
	}
	stored := a.hist.Get(job.ChatID)
	stored = a.maybeCompact(ctx, job.ChatID, stored)

	label := strings.TrimSpace(job.Name)
	if label == "" {
		label = job.ID
	}
	prompt := fmt.Sprintf("[Scheduled task %s]\n%s", label, strings.TrimSpace(job.Prompt))
	opts := &ai.ProcessOptions{ChatID: job.ChatID}
	if job.UserID != 0 {
		opts.User = &ai.TelegramUser{ID: job.UserID}
	}
	res := a.agent.ProcessMessage(ctx, prompt, stored, opts)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	saved := make([]*messages.Message, 0, len(stored)+1+len(res.ResponseMessages))
	saved = append(saved, stored...)
	userMsg := &messages.Message{Role: "user"}
	messages.SetContentString(userMsg, prompt)
	saved = append(saved, userMsg)
	saved = append(saved, messages.SanitizeHistoryMessages(res.ResponseMessages)...)
	_ = a.hist.Set(job.ChatID, saved)

	text := strings.TrimSpace(res.Text)
	if text == "" {
		text = "Scheduled task finished with no output."
	}
	header := "⏰ " + label + "\n"
	if a.tg == nil {
		log.Printf("[schedule] no telegram client, result for chat %d: %.120s", job.ChatID, text)
		return nil
	}
	full := header + text
	if len(full) > maxMessageLength {
		_ = a.tg.SendFile(ctx, job.ChatID, "schedule.md", []byte(full), "Scheduled task output.")
		return nil
	}
	_, err := a.tg.SendMessage(ctx, job.ChatID, full, nil)
	return err
}

// FormatScheduleList renders jobs for /sched output.
func FormatScheduleList(jobs []schedule.Job) string {
	if len(jobs) == 0 {
		return "No scheduled jobs. Ask me to schedule something, e.g. \"every day at 6am WIB check stock prices\"."
	}
	lines := make([]string, 0, len(jobs))
	for _, j := range jobs {
		lines = append(lines, "• "+j.Describe())
	}
	return "⏰ Scheduled jobs:\n" + strings.Join(lines, "\n")
}
