// Package memory: compact summarizer for the lightweight assistant.
//
// Flow (no history trimming anywhere else):
//   - Before each new prompt, the app counts history tokens.
//   - If history >= HistoryTokenLimit (default 30k), Compact is called:
//     the model summarizes old MEMORY.md + full history into a compact
//     MEMORY.md (bullet points only, 3 sections), then history is wiped clean.
//
// MEMORY.md style: concise "-", 3 sections: Complete, Active, Relevant Files.
package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/tmc/langchaingo/llms"
)

const memoryMaxOutput = 2000

const memoryPrompt = `You are the memory compactor for local assistant "PURU-AI".
Read the old MEMORY.md and the full conversation, then write a new compact MEMORY.md.

Rules:
- Output ONLY markdown, concise bullet points with "- " prefix. No preamble, no prose paragraphs.
- Exactly these 3 sections, in order:
  ## Complete
  ## Active
  ## Relevant Files
- Complete = finished tasks/decisions (bullets, or "- none").
- Active = ongoing threads, open todos, user preferences (bullets).
- Relevant Files = workspace paths worth remembering with one-line why (bullets, or "- none").
- Drop everything outdated/irrelevant. Keep it tight, max 2000 chars.`

func noopStream(context.Context, []byte) error { return nil }

type Manager struct {
	Model      llms.Model
	MemoryPath string
	MaxOutput  int
}

func New(model llms.Model, memoryPath string) *Manager {
	return &Manager{Model: model, MemoryPath: memoryPath, MaxOutput: memoryMaxOutput}
}

func messageText(m interface{ Text() string }) string { return m.Text() }

func dirOf(p string) string { return filepath.Dir(p) }

// Compact summarizes old memory + history into MEMORY.md.
// Returns new content ("" when nothing produced).
func (m *Manager) Compact(ctx context.Context, oldMemory string, historyText string) (string, error) {
	if strings.TrimSpace(historyText) == "" {
		return "", nil
	}
	maxOut := m.MaxOutput
	if maxOut <= 0 {
		maxOut = memoryMaxOutput
	}
	res, err := m.Model.GenerateContent(ctx, []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextContent{Text: memoryPrompt}}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{
			Text: "MEMORY.md lama:\n" + oldMemory + "\n\nPercakapan:\n" + historyText,
		}}},
	}, llms.WithMaxTokens(maxOut), llms.WithStreamingFunc(noopStream))
	if err != nil {
		return "", err
	}
	text := ""
	if res != nil && len(res.Choices) > 0 {
		text = res.Choices[0].Content
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", nil
	}
	if err := os.MkdirAll(dirOf(m.MemoryPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(m.MemoryPath, []byte(trimmed), 0o644); err != nil {
		return "", err
	}
	return trimmed, nil
}

// Read returns current MEMORY.md ("" when missing).
func (m *Manager) Read() string {
	b, err := os.ReadFile(m.MemoryPath)
	if err != nil {
		return ""
	}
	return string(b)
}
