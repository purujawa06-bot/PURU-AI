// CLI debug for the lightweight assistant (no Telegram).
// Usage: go run ./cmd/cli "message..." | go run ./cmd/cli (REPL)
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/purujawa06-bot/PURU-AI/internal/ai"
	"github.com/purujawa06-bot/PURU-AI/internal/config"
	"github.com/purujawa06-bot/PURU-AI/internal/history"
	"github.com/purujawa06-bot/PURU-AI/internal/memory"
	"github.com/purujawa06-bot/PURU-AI/internal/messages"
	"github.com/purujawa06-bot/PURU-AI/internal/prompt"
)

func main() {
	debug.SetMemoryLimit(50 << 20)

	// Reap orphaned exec grandchildren (PID 1 zombie collector, unix only).
	ai.StartReaper()

	cfgPath := flag.String("config", "", "path to config.json")
	chatID := flag.Int64("chat", -777, "debug chat id")
	reset := flag.Bool("reset", false, "clear history and exit")
	flag.Parse()

	cfg, err := config.Load(config.ResolvePath(*cfgPath))
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	hc := &http.Client{} // No timeout, let the API decide
	llm, err := ai.NewModel(cfg, hc)
	if err != nil {
		log.Fatalf("ai model: %v", err)
	}
	hist := history.New(cfg.HistoryDir())
	mem := memory.New(cfg.Workspace)
	mem.Model = llm
	agent := &ai.Agent{Client: llm, Config: cfg, HTTP: hc}
	ctx := context.Background()

	if *reset {
		_ = hist.Clear(*chatID)
		fmt.Printf("Reset chat=%d.\n", *chatID)
		return
	}
	if flag.NArg() == 0 {
		fmt.Printf("CLI PURU-AI — chat=%d — /exit to quit, /reset to clear history\n", *chatID)
		sc := bufio.NewScanner(os.Stdin)
		for {
			fmt.Print("You > ")
			if !sc.Scan() {
				break
			}
			line := strings.TrimSpace(sc.Text())
			if line == "/exit" || line == "/quit" {
				return
			}
			if line == "/reset" {
				_ = hist.Clear(*chatID)
				fmt.Println("History cleared.")
				continue
			}
			if line == "" {
				continue
			}
			fmt.Printf("\nPURU-AI > %s\n\n", strings.TrimSpace(process(ctx, agent, hist, mem, cfg, *chatID, line)))
		}
		return
	}
	fmt.Println(strings.TrimSpace(process(ctx, agent, hist, mem, cfg, *chatID, strings.Join(flag.Args(), " "))))
}

func process(ctx context.Context, agent *ai.Agent, hist *history.Store, mem *memory.Manager, cfg *config.Config, chatID int64, prompt string) string {
	stored := hist.Get(chatID)
	if history.TokenCountFull(renderedSystemPrompt(cfg), stored) >= cfg.HistoryTokenLimit && len(stored) > 0 {
		cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		rel, cerr := mem.Compact(cctx, stored)
		cancel()
		if cerr != nil {
			log.Printf("compact: %v", cerr)
		} else if rel != "" {
			stored = []*messages.Message{}
			_ = hist.Set(chatID, stored)
			fmt.Printf("(saved: %s)\n", rel)
		}
	}
	opts := &ai.ProcessOptions{ChatID: chatID}
	if cfg.ShowToolsPreview() {
		opts.OnTool = func(name string, args map[string]any) {
			fmt.Printf("🔧 %s\n", name)
		}
	}
	res := agent.ProcessMessage(ctx, prompt, stored, opts)
	saved := append(append([]*messages.Message{}, stored...), userMsg(prompt)...)
	// Simpan apa adanya; tanpa prune — biarkan compact yang bekerja.
	saved = append(saved, messages.SanitizeHistoryMessages(res.ResponseMessages)...)
	_ = hist.Set(chatID, saved)
	return res.Text
}

// renderedSystemPrompt renders the same system prompt the agent sends so
// token counting matches the real request context (MEMORY.md + latest
// context/*.md summary).
func renderedSystemPrompt(cfg *config.Config) string {
	mem := ""
	if b, err := os.ReadFile(cfg.MemoryPath()); err == nil {
		mem = string(b)
	}
	s, err := prompt.Get(mem, memory.LatestSummary(cfg.Workspace), cfg.Workspace, cfg.SkillsPolicy())
	if err != nil {
		return ""
	}
	return s
}

func userMsg(s string) []*messages.Message {
	m := &messages.Message{Role: "user"}
	messages.SetContentString(m, s)
	return []*messages.Message{m}
}
