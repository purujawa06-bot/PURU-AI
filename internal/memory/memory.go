// Package memory: model-generated conversation summaries.
//
// Flow (no history trimming anywhere else):
//   - Before each new prompt, the app counts history tokens.
//   - If history >= HistoryTokenLimit (default 30k), Compact is called:
//     the model summarizes the full history into one markdown file under
//     <workspace>/memory/context/YYYY-MM-DD_HH-MM-SS.md, only the newest 20
//     files are kept, then history is wiped clean.
//   - The NEWEST summary is injected into the system prompt on every
//     request (see LatestSummary), so the AI keeps long-term context.
//     Older summaries stay in memory/context/ for reference only.
//   - On summarize failure Compact returns an error and history is kept
//     as-is (the next message retries).
//
// memory/MEMORY.md is NEVER touched here: it holds lasting user facts
// (name, hobby, personal info) written by the agent itself.
package memory

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tmc/langchaingo/llms"

	"github.com/purujawa06-bot/PURU-AI/internal/messages"
)

// MaxSummaries keeps only this many newest files in memory/context/.
const MaxSummaries = 20

// summarizePrompt asks the model for a compact markdown summary of a
// conversation transcript. English: summaries are injected into the
// English system prompt.
const summarizePrompt = `Summarize the conversation below into concise markdown (max ~400 words). ` +
	`Focus on extracting lasting facts, task progress, and pending items. ` +
	`Sections: ## Key facts (stable user info, preferences, decisions), ` +
	`## Done (tasks completed + specific outcomes), ## Pending (open tasks, unanswered questions), ` +
	`## Notes (technical details or context for future turns). Skip empty sections. No preamble, just the markdown.\n\n`

type Manager struct {
	Workspace string
	// Model summarizes history (retryModel: non-streaming call, 5x retry).
	// Nil = Compact fails closed (history is kept, next message retries).
	Model llms.Model
}

func New(workspace string) *Manager {
	return &Manager{Workspace: workspace}
}

// ContextDir is <workspace>/memory/context — where summary files live.
func (m *Manager) ContextDir() string {
	return filepath.Join(m.Workspace, "memory", "context")
}

// Compact summarizes msgs with the model into
// memory/context/YYYY-MM-DD_HH-MM-SS.md (date+time only), prunes old files to
// MaxSummaries newest, and returns the workspace-relative path (e.g.
// "memory/context/2026-09-21_14-05-30.md"). memory/MEMORY.md is never touched.
// Returns "" when there is nothing to dump; returns an error (history kept)
// when no model is set or summarization fails.
func (m *Manager) Compact(ctx context.Context, msgs []*messages.Message) (string, error) {
	var live []*messages.Message
	for _, msg := range msgs {
		if msg != nil {
			live = append(live, msg)
		}
	}
	if len(live) == 0 {
		return "", nil
	}
	if m.Model == nil {
		return "", errNoModel
	}
	summary, err := m.summarize(ctx, live)
	if err != nil {
		return "", err
	}
	rel, err := m.saveSummary(summary)
	if err != nil {
		return "", err
	}
	m.prune()
	return rel, nil
}

// summarize calls the model once (non-streaming) over the transcript.
func (m *Manager) summarize(ctx context.Context, msgs []*messages.Message) (string, error) {
	resp, err := m.Model.GenerateContent(ctx,
		[]llms.MessageContent{{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextContent{Text: summarizePrompt + historyText(msgs)}},
		}})
	if err != nil {
		return "", err
	}
	if resp == nil || len(resp.Choices) == 0 {
		return "", errEmptySummary
	}
	out := strings.TrimSpace(resp.Choices[0].Content)
	if out == "" {
		return "", errEmptySummary
	}
	return out + "\n", nil
}

// historyText flattens messages to "role: text" lines, including tool-call
// names+args and tool-result outputs (reasoning excluded).
func historyText(msgs []*messages.Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		if m == nil {
			continue
		}
		text := m.Text()
		if messages.IsParts(m) {
			for _, p := range messages.ContentParts(m) {
				switch p.Type() {
				case "tool-call":
					if t := p.ToolCallText(); t != "" {
						text += "\n[tool-call] " + t
					}
				case "tool-result":
					if t := p.ResultText(); t != "" {
						// Context optimization: limit tool result length sent to summarizer
						// to prevent large outputs from bloating the summary request.
						if len(t) > 1000 {
							t = t[:1000] + " ... [truncated for summary]"
						}
						text += "\n[tool-result] " + t
					}
				}
			}
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		sb.WriteString(m.Role + ": " + text + "\n\n")
	}
	return sb.String()
}

// Latest returns the content of the newest summary in memory/context/ ("" when
// none). Injected into the system prompt on every request.
func (m *Manager) Latest() string { return LatestSummary(m.Workspace) }

// LatestSummary reads the newest *.md summary in <workspace>/memory/context.
func LatestSummary(workspace string) string {
	dir := filepath.Join(workspace, "memory", "context")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best := ""
	var bestMod time.Time
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".md") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if best == "" || info.ModTime().After(bestMod) {
			best, bestMod = n, info.ModTime()
		}
	}
	if best == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, best))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// saveSummary writes the file (deduping name collisions on same-second
// compactions) and returns the workspace-relative path with forward slashes.
func (m *Manager) saveSummary(summary string) (string, error) {
	dir := m.ContextDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	stamp := time.Now().Format("2006-01-02_15-04-05")
	name := stamp + ".md"
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
			break
		}
		name = stamp + "-" + strconv.Itoa(i) + ".md"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(summary), 0o644); err != nil {
		return "", err
	}
	return "memory/context/" + name, nil
}

// prune deletes oldest files beyond MaxSummaries newest (best-effort).
// Old .json dumps (from before the summarize era) count toward the cap
// too so they age out naturally.
func (m *Manager) prune() {
	entries, err := os.ReadDir(m.ContextDir())
	if err != nil {
		return
	}
	type fi struct {
		name string
		mod  time.Time
	}
	var files []fi
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || (!strings.HasSuffix(n, ".md") && !strings.HasSuffix(n, ".json")) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fi{n, info.ModTime()})
	}
	if len(files) <= MaxSummaries {
		return
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].mod.Equal(files[j].mod) {
			return files[i].name > files[j].name
		}
		return files[i].mod.After(files[j].mod)
	})
	for _, f := range files[MaxSummaries:] {
		if err := os.Remove(filepath.Join(m.ContextDir(), f.name)); err != nil {
			log.Printf("[memory] prune %s: %v", f.name, err)
		}
	}
}
