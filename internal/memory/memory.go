// Package memory: compact summarizer for the lightweight assistant.
//
// Flow (no history trimming anywhere else):
//   - Before each new prompt, the app counts history tokens.
//   - If history >= HistoryTokenLimit (default 30k), Compact is called:
//     the model summarizes the full history into one file under
//     <workspace>/context/YYYY-MM-DD_slug.md, only the newest 20 summary
//     files are kept, then history is wiped clean.
//   - The AI is NOT given the summary content — only the file path is
//     injected as a system note, so it can read that file (or older ones
//     in context/) with read_file when old context is needed.
//
// MEMORY.md is NEVER touched here: it holds lasting user facts
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
)

// MaxSummaries keeps only this many newest files in context/.
const MaxSummaries = 20

const summaryMaxOutput = 4000

const summaryPrompt = `You are the memory compactor for local assistant "PURU-AI".
Summarize the conversation below into a compact file summary.

Rules:
- First line MUST be: TITLE: <short title, max 6 words, no quotes>
- Then a blank line, then concise bullet points with "- " prefix. No preamble, no prose paragraphs.
- Bullets cover: finished tasks/decisions, ongoing threads and open todos, important facts, workspace paths worth remembering (with one-line why).
- Drop chatter and outdated stuff. Keep it tight.`

func noopStream(context.Context, []byte) error { return nil }

type Manager struct {
	Model     llms.Model
	Workspace string
	MaxOutput int
}

func New(model llms.Model, workspace string) *Manager {
	return &Manager{Model: model, Workspace: workspace, MaxOutput: summaryMaxOutput}
}

// ContextDir is <workspace>/context — where summary files live.
func (m *Manager) ContextDir() string { return filepath.Join(m.Workspace, "context") }

// Compact summarizes historyText into context/YYYY-MM-DD_slug.md, prunes old
// summaries to MaxSummaries newest, and returns the workspace-relative path
// (e.g. "context/2026-09-21_belajar-go.md"). MEMORY.md is never touched.
// Returns "" when there is nothing to summarize.
func (m *Manager) Compact(ctx context.Context, historyText string) (string, error) {
	if strings.TrimSpace(historyText) == "" {
		return "", nil
	}
	maxOut := m.MaxOutput
	if maxOut <= 0 {
		maxOut = summaryMaxOutput
	}
	res, err := m.Model.GenerateContent(ctx, []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextContent{Text: summaryPrompt}}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: historyText}}},
	}, llms.WithMaxTokens(maxOut), llms.WithStreamingFunc(noopStream))
	if err != nil {
		return "", err
	}
	text := ""
	if res != nil && len(res.Choices) > 0 {
		text = res.Choices[0].Content
	}
	title, body := splitTitle(strings.TrimSpace(text))
	if body == "" {
		return "", nil
	}
	rel, err := m.saveSummary(title, body)
	if err != nil {
		return "", err
	}
	m.prune()
	return rel, nil
}

// SummaryNote is the system note injected after compaction: path only,
// never the summary content.
func SummaryNote(rel string) string {
	return "Percakapan sebelumnya telah diringkas di " + rel +
		". Baca file itu dengan read_file bila butuh konteks lama; ringkasan lain ada di folder context/."
}

// splitTitle extracts the TITLE: first line; falls back to "ringkasan".
func splitTitle(s string) (title, body string) {
	if s == "" {
		return "", ""
	}
	lines := strings.SplitN(s, "\n", 2)
	first := strings.TrimSpace(lines[0])
	rest := ""
	if len(lines) > 1 {
		rest = strings.TrimSpace(lines[1])
	}
	if t, ok := strings.CutPrefix(first, "TITLE:"); ok && strings.TrimSpace(t) != "" {
		if rest == "" {
			return strings.TrimSpace(t), strings.TrimSpace(t)
		}
		return strings.TrimSpace(t), rest
	}
	return "ringkasan", s
}

// slugify makes a filename-safe slug, max 40 chars.
func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevDash := true // avoid leading dash
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 40 {
		out = strings.Trim(out[:40], "-")
	}
	if out == "" {
		out = "ringkasan"
	}
	return out
}

// saveSummary writes the file (deduping name collisions) and returns the
// workspace-relative path with forward slashes.
func (m *Manager) saveSummary(title, body string) (string, error) {
	dir := m.ContextDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	date := time.Now().Format("2006-01-02")
	base := date + "_" + slugify(title)
	name := base + ".md"
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
			break
		}
		name = base + "-" + strconv.Itoa(i) + ".md"
	}
	content := "# " + title + "\n\nTanggal: " + date + "\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		return "", err
	}
	return "context/" + name, nil
}

// prune deletes oldest summaries beyond MaxSummaries newest (best-effort).
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
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fi{e.Name(), info.ModTime()})
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
