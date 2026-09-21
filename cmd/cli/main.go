// CLI debug for the lightweight assistant (no Telegram).
// Usage: go run ./cmd/cli "pesan..." | go run ./cmd/cli (REPL)
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

	cfgPath := flag.String("config", "", "path config.json")
	chatID := flag.Int64("chat", -777, "chat id debug")
	reset := flag.Bool("reset", false, "hapus history lalu exit")
	flag.Parse()

	cfg, err := config.Load(config.ResolvePath(*cfgPath))
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	hc := &http.Client{Timeout: 60 * time.Second}
	llm, err := ai.NewModel(cfg, hc)
	if err != nil {
		log.Fatalf("ai model: %v", err)
	}
	hist := history.New(cfg.HistoryDir())
	mem := memory.New(llm, cfg.Workspace)
	agent := &ai.Agent{Client: llm, Config: cfg, HTTP: hc}
	ctx := context.Background()

	if *reset {
		_ = hist.Clear(*chatID)
		fmt.Printf("Reset chat=%d.\n", *chatID)
		return
	}
	if flag.NArg() == 0 {
		fmt.Printf("CLI PURU-AI — chat=%d — /exit keluar, /reset hapus history\n", *chatID)
		sc := bufio.NewScanner(os.Stdin)
		for {
			fmt.Print("Anda > ")
			if !sc.Scan() {
				break
			}
			line := strings.TrimSpace(sc.Text())
			if line == "/exit" || line == "/quit" {
				return
			}
			if line == "/reset" {
				_ = hist.Clear(*chatID)
				fmt.Println("History dihapus.")
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
		var sb strings.Builder
		for _, m := range stored {
			if m == nil {
				continue
			}
			sb.WriteString(m.Role + ": " + m.Text() + "\n")
		}
		cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		rel, cerr := mem.Compact(cctx, sb.String())
		cancel()
		if cerr != nil {
			log.Printf("compact: %v", cerr)
		} else if rel != "" {
			_ = hist.Clear(chatID)
			note := &messages.Message{Role: "system"}
			messages.SetContentString(note, memory.SummaryNote(rel))
			_ = hist.Set(chatID, []*messages.Message{note})
			stored = []*messages.Message{note}
			fmt.Printf("(ringkasan: %s)\n", rel)
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
	saved = append(saved, messages.SanitizeHistoryMessages(res.ResponseMessages)...)
	_ = hist.Set(chatID, saved)
	return res.Text
}

// renderedSystemPrompt renders the same system prompt the agent sends so
// token counting matches the real request context.
func renderedSystemPrompt(cfg *config.Config) string {
	mem := ""
	if b, err := os.ReadFile(cfg.MemoryPath()); err == nil {
		mem = string(b)
	}
	s, err := prompt.Get(mem)
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
