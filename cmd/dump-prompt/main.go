// Command dump-prompt renders one representative system prompt and writes
// it to SYSTEMPROMPT.md. It exists so the layered prompt built by
// internal/prompt can be inspected on GitHub Actions runners (no Go
// toolchain is required on the dev machine).
//
// Usage:
//
//	go run ./cmd/dump-prompt [--out SYSTEMPROMPT.md]
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/purujawa06-bot/PURU-AI/internal/prompt"
	"github.com/purujawa06-bot/PURU-AI/internal/workspace"
)

const sampleMemory = `# Long-term Memory

- User name: Ricky (20 tahun)
- Timezone: Asia/Jakarta (WIB, UTC+7)
- Preference: Always apply rules-write-code skill for any code writing/editing/refactoring task`

const sampleSummary = `Prior session: user asked what changed vs what was kept across the last two commits (workspace defaults mirror + layered prompt port). Assistant summarized AGENTS/MEMORY/USER diffs and the prompt builder rewrite.`

func main() {
	outPath := flag.String("out", "SYSTEMPROMPT.md", "output markdown file for the rendered system prompt")
	flag.Parse()

	workspaceDir, err := setupDebugWorkspace()
	if err != nil {
		log.Fatalf("setup workspace: %v", err)
	}

	rendered, err := prompt.Build(prompt.Request{
		Workspace:         workspaceDir,
		Memory:            sampleMemory,
		Summary:           sampleSummary,
		Channel:           "telegram",
		ChatID:            "123456",
		SenderID:          "7",
		SenderDisplayName: "Ricky (@ricky)",
		Policy:            workspace.SkillsPolicy{},
	})
	if err != nil {
		log.Fatalf("prompt.Build: %v", err)
	}

	if err := os.WriteFile(*outPath, []byte(rendered+"\n"), 0o644); err != nil {
		log.Fatalf("write output: %v", err)
	}

	parts := strings.Split(rendered, "\n\n---\n\n")
	fmt.Printf("workspace=%s\n", workspaceDir)
	fmt.Printf("out=%s chars=%d parts=%d time=%s\n", *outPath, len(rendered), len(parts), time.Now().Format(time.RFC3339))
	fmt.Printf("markers:\n")
	for _, marker := range []string{
		"# PuruClaw 🦞",
		"## " + workspace.FileAgents,
		"## " + workspace.FileSoul,
		"## " + workspace.FileUser,
		"# Skills",
		"<skills>",
		"# Active Skills",
		"### Skill: find-skills",
		"# Memory",
		"memory/MEMORY.md",
		"## Current Time",
		"## Runtime",
		"## Current Session",
		"Channel: telegram",
		"CONTEXT_SUMMARY:",
		"ALWAYS use tools",
	} {
		fmt.Printf("  has %-28q %v\n", marker, strings.Contains(rendered, marker))
	}
}

// setupDebugWorkspace creates a temp workspace seeded from embedded defaults,
// then injects a skills frontmatter so the dump shows both the skill catalog
// and one active skill body (the fullest production shape).
func setupDebugWorkspace() (string, error) {
	dir, err := os.MkdirTemp("", "puru-prompt-dump-")
	if err != nil {
		return "", err
	}
	if err := workspace.Ensure(dir); err != nil {
		return "", err
	}

	body := defaultAgentsBody()
	agents := "---\nskills: [find-skills]\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, workspace.FileAgents), []byte(agents), 0o644); err != nil {
		return "", err
	}
	return dir, nil
}

// defaultAgentsBody returns the embedded default agent body without its YAML
// frontmatter so the debug frontmatter above does not duplicate it.
func defaultAgentsBody() string {
	raw := strings.ReplaceAll(workspace.DefaultAgentsMD, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				return strings.TrimLeft(strings.Join(lines[i+1:], "\n"), "\n")
			}
		}
	}
	return raw
}
