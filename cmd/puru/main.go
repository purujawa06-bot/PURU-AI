// Command puru is the npm-friendly CLI for PURU-AI (no web server by default).
//
// Usage:
//
//	puru setup [--config PATH] [--force]   — interactive wizard, writes config.json
//	puru gateway [--config PATH] [--health] — run Telegram bot (no /health unless --health)
//	puru chat "pesan..." [--config PATH] [--chat ID] [--reset] — local debug, no Telegram
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/purujawa06-bot/PURU-AI/internal/ai"
	"github.com/purujawa06-bot/PURU-AI/internal/app"
	"github.com/purujawa06-bot/PURU-AI/internal/config"
	"github.com/purujawa06-bot/PURU-AI/internal/health"
	"github.com/purujawa06-bot/PURU-AI/internal/history"
	"github.com/purujawa06-bot/PURU-AI/internal/memory"
	"github.com/purujawa06-bot/PURU-AI/internal/messages"
	"github.com/purujawa06-bot/PURU-AI/internal/prompt"
	"github.com/purujawa06-bot/PURU-AI/internal/telegram"
)

// version is injected at release time: -ldflags="-X main.version=v1.2.3".
var version = "dev"

func main() {
	debug.SetMemoryLimit(50 << 20)

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "setup":
		if err := runSetup(os.Args[2:]); err != nil {
			log.Fatalf("setup: %v", err)
		}
	case "gateway":
		if err := runGateway(os.Args[2:]); err != nil {
			log.Fatalf("gateway: %v", err)
		}
	case "chat":
		if err := runChat(os.Args[2:]); err != nil {
			log.Fatalf("chat: %v", err)
		}
	case "-h", "--help", "help":
		usage()
	case "-v", "--version", "version":
		fmt.Printf("puru %s\n", version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Printf(`puru %s — PURU-AI CLI (tanpa web server kecuali opt-in)

Usage:
  puru setup [--config PATH] [--force]
    Wizard interaktif, tulis config.json (default %s).
    Env non-interaktif: TELEGRAM_BOT_TOKEN, PURU_BASE_URL, PURU_API_KEY,
    PURU_MODEL, PURU_WORKSPACE. Tambah --force untuk timpa tanpa tanya.

  puru gateway [--config PATH] [--health] [--host H] [--port P]
    Jalankan bot Telegram via long-polling. TANPA web server secara default.
    Tambah --health untuk menyalakan GET /health (Docker / VPS).

  puru chat "pesan..." [--config PATH] [--chat ID] [--reset]
    Debug lokal tanpa Telegram (REPL bila tanpa argumen).

Flags global:
  --config PATH   path config.json (default ~/.puru/config.json)

Contoh:
  puru setup
  puru gateway --config ~/.puru/config.json
  puru gateway --health --port 8080
`, version, config.DefaultPath())
}

// ---------- setup ----------

type setupOptions struct {
	configPath string
	force      bool
}

func parseSetupArgs(args []string) (*setupOptions, error) {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	o := &setupOptions{}
	fs.StringVar(&o.configPath, "config", "", "path config.json")
	fs.BoolVar(&o.force, "force", false, "timpa config tanpa konfirmasi")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("argumen berlebih: %s", strings.Join(fs.Args(), " "))
	}
	return o, nil
}

func runSetup(args []string) error {
	o, err := parseSetupArgs(args)
	if err != nil {
		return err
	}
	path := config.ResolvePath(o.configPath)
	if _, err := os.Stat(path); err == nil && !o.force {
		if !askYes(fmt.Sprintf("Config %s sudah ada. Timpa? [y/N]: ", path), false) {
			fmt.Println("Batal, config tidak diubah.")
			return nil
		}
	}
	in := bufio.NewReader(os.Stdin)
	nonInteractive := os.Getenv("PURU_NON_INTERACTIVE") != "" || !isTerminal(in)

	token := firstNonEmpty(os.Getenv("TELEGRAM_BOT_TOKEN"), "")
	baseURL := firstNonEmpty(os.Getenv("PURU_BASE_URL"), "https://betatestervueui2-b.hf.space/v1")
	apiKey := firstNonEmpty(os.Getenv("PURU_API_KEY"), "")
	model := firstNonEmpty(os.Getenv("PURU_MODEL"), "puru")
	workspace := firstNonEmpty(os.Getenv("PURU_WORKSPACE"), filepath.Join(config.DefaultDir(), "workspace"))

	if !nonInteractive {
		fmt.Println("== PURU-AI setup ==")
		token = askLine(in, "Telegram bot token (dari @BotFather)", token, true)
		baseURL = askLine(in, "Model base_url (OpenAI-compatible)", baseURL, true)
		apiKey = askLine(in, "Model api_key", apiKey, true)
		model = askLine(in, "Model name", model, true)
		workspace = askLine(in, "Workspace dir", workspace, true)
	}
	if strings.TrimSpace(token) == "" {
		return errors.New("telegram_bot_token wajib diisi (env TELEGRAM_BOT_TOKEN)")
	}
	if strings.TrimSpace(baseURL) == "" {
		return errors.New("model.base_url wajib diisi (env PURU_BASE_URL)")
	}
	if strings.TrimSpace(model) == "" {
		return errors.New("model.model wajib diisi (env PURU_MODEL)")
	}

	cfg := map[string]any{
		"telegram_bot_token":     strings.TrimSpace(token),
		"telegram_allowed_users": []int64{},
		"model": map[string]any{
			"base_url":    strings.TrimSpace(baseURL),
			"api_key":     strings.TrimSpace(apiKey),
			"model":       strings.TrimSpace(model),
			"temperature": 0,
		},
		"workspace":           strings.TrimSpace(workspace),
		"restrict_workspace":  true,
		"max_iterations":      config.DefaultMaxIterations,
		"history_token_limit": config.DefaultHistoryTokLimit,
		"host":                config.DefaultHealthHost,
		"port":                config.DefaultHealthPort,
		"tools_preview":       true,
		"loop_delay_seconds":  config.DefaultLoopDelaySeconds,
		"exec_memory_mb":      config.DefaultExecMemoryMB,
	}
	if err := writeConfigJSON(path, cfg); err != nil {
		return err
	}
	// Validate by loading (also creates workspace + history dirs).
	if _, err := config.Load(path); err != nil {
		return fmt.Errorf("config tertulis tapi tidak valid: %w", err)
	}
	fmt.Printf("OK config tersimpan: %s\n", path)
	fmt.Println("Lanjut: puru gateway --config " + path)
	return nil
}

func writeConfigJSON(path string, v map[string]any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func askLine(in *bufio.Reader, label, def string, required bool) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := in.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func askYes(label string, def bool) bool {
	// Helper returns bool via string compare; kept simple for testability.
	fmt.Print(label)
	var line string
	_, _ = fmt.Scanln(&line)
	return normalizeYes(line, def)
}

// normalizeYes is split out for unit tests.
func normalizeYes(s string, def bool) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "y", "yes", "ya":
		return true
	case "n", "no", "tidak", "":
		if s == "" {
			return def
		}
		return false
	default:
		return def
	}
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func isTerminal(r *bufio.Reader) bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// ---------- gateway (tanpa web server by default) ----------

type gatewayOptions struct {
	configPath string
	withHealth bool
	host       string
	port       int
}

func parseGatewayArgs(args []string) (*gatewayOptions, error) {
	fs := flag.NewFlagSet("gateway", flag.ContinueOnError)
	o := &gatewayOptions{}
	fs.StringVar(&o.configPath, "config", "", "path config.json")
	fs.BoolVar(&o.withHealth, "health", false, "nyalakan GET /health")
	fs.StringVar(&o.host, "host", "", "override host health (default dari config)")
	fs.IntVar(&o.port, "port", 0, "override port health (default dari config)")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("argumen berlebih: %s", strings.Join(fs.Args(), " "))
	}
	return o, nil
}

func runGateway(args []string) error {
	o, err := parseGatewayArgs(args)
	if err != nil {
		return err
	}
	cfg, err := config.Load(config.ResolvePath(o.configPath))
	if err != nil {
		return err
	}
	hc := &http.Client{}

	llm, err := ai.NewModel(cfg, hc)
	if err != nil {
		return fmt.Errorf("ai model: %w", err)
	}
	histStore := history.New(cfg.HistoryDir())
	memSvc := memory.New(cfg.Workspace)
	memSvc.Model = llm
	tg, err := telegram.New(cfg.TelegramBotToken, hc)
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}
	agentSvc := &ai.Agent{Client: llm, Config: cfg, HTTP: hc, Telegram: tg}
	appSvc := app.New(cfg, tg, histStore, agentSvc, memSvc)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Health check HANYA bila --health. Versi CLI/npm default tanpa web server.
	if o.withHealth {
		host, port := cfg.Host, cfg.Port
		if o.host != "" {
			host = o.host
		}
		if o.port > 0 {
			port = o.port
		}
		addr := health.Addr(host, port)
		go func() {
			log.Printf("health: %s/health", addr)
			if err := health.Serve(addr); err != nil {
				log.Printf("health: %v", err)
			}
		}()
	}

	me, err := tg.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("cannot reach Telegram API: %w", err)
	}
	log.Printf("PURU-AI %s on @%s (workspace=%s, max_iter=%d, health=%v)",
		version, me.Username, cfg.Workspace, cfg.MaxIterations, o.withHealth)

	if err := tg.DeleteWebhook(ctx, true); err != nil {
		log.Printf("deleteWebhook: %v", err)
	}
	if err := tg.SetCommands(ctx); err != nil {
		log.Printf("setMyCommands: %v", err)
	}

	var offset int64
	conflicts := 0
	for {
		select {
		case <-ctx.Done():
			log.Printf("gateway: shutdown")
			return nil
		default:
		}
		updates, err := tg.GetUpdates(ctx, offset, 40)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var te *telegram.TelegramError
			if errors.As(err, &te) && te.IsConflict() {
				conflicts++
				if conflicts >= 5 {
					return fmt.Errorf("conflict %dx — instance lain memakai token", conflicts)
				}
				time.Sleep(10 * time.Second)
				_ = tg.DeleteWebhook(ctx, true)
				offset = 0
				continue
			}
			log.Printf("getUpdates: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		conflicts = 0
		for _, u := range updates {
			offset = u.UpdateID + 1
			if err := appSvc.Handle(ctx, &u); err != nil {
				log.Printf("handle %d: %v", u.UpdateID, err)
			}
		}
	}
}

// ---------- chat (debug lokal, tanpa Telegram) ----------

type chatOptions struct {
	configPath string
	chatID     int64
	reset      bool
}

func parseChatArgs(args []string) (*chatOptions, []string, error) {
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	o := &chatOptions{}
	fs.StringVar(&o.configPath, "config", "", "path config.json")
	fs.Int64Var(&o.chatID, "chat", -777, "chat id debug")
	fs.BoolVar(&o.reset, "reset", false, "hapus history lalu exit")
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	return o, fs.Args(), nil
}

func runChat(args []string) error {
	o, rest, err := parseChatArgs(args)
	if err != nil {
		return err
	}
	cfg, err := config.Load(config.ResolvePath(o.configPath))
	if err != nil {
		return err
	}
	hc := &http.Client{}
	llm, err := ai.NewModel(cfg, hc)
	if err != nil {
		return fmt.Errorf("ai model: %w", err)
	}
	hist := history.New(cfg.HistoryDir())
	mem := memory.New(cfg.Workspace)
	mem.Model = llm
	agent := &ai.Agent{Client: llm, Config: cfg, HTTP: hc}
	ctx := context.Background()

	if o.reset {
		_ = hist.Clear(o.chatID)
		fmt.Printf("Reset chat=%d.\n", o.chatID)
		return nil
	}
	if len(rest) == 0 {
		fmt.Printf("CLI PURU-AI — chat=%d — /exit keluar, /reset hapus history\n", o.chatID)
		sc := bufio.NewScanner(os.Stdin)
		for {
			fmt.Print("Anda > ")
			if !sc.Scan() {
				return nil
			}
			line := strings.TrimSpace(sc.Text())
			if line == "/exit" || line == "/quit" {
				return nil
			}
			if line == "/reset" {
				_ = hist.Clear(o.chatID)
				fmt.Println("History dihapus.")
				continue
			}
			if line == "" {
				continue
			}
			fmt.Printf("\nPURU-AI > %s\n\n", strings.TrimSpace(processChat(ctx, agent, hist, mem, cfg, o.chatID, line)))
		}
	}
	fmt.Println(strings.TrimSpace(processChat(ctx, agent, hist, mem, cfg, o.chatID, strings.Join(rest, " "))))
	return nil
}

func processChat(ctx context.Context, agent *ai.Agent, hist *history.Store, mem *memory.Manager, cfg *config.Config, chatID int64, p string) string {
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
			fmt.Printf("(tersimpan: %s)\n", rel)
		}
	}
	opts := &ai.ProcessOptions{ChatID: chatID}
	if cfg.ShowToolsPreview() {
		opts.OnTool = func(name string, args map[string]any) {
			fmt.Printf("🔧 %s\n", name)
		}
	}
	res := agent.ProcessMessage(ctx, p, stored, opts)
	saved := append(append([]*messages.Message{}, stored...), userMsg(p)...)
	saved = append(saved, messages.SanitizeHistoryMessages(res.ResponseMessages)...)
	saved = messages.PruneTurn(saved)
	_ = hist.Set(chatID, saved)
	return res.Text
}

func renderedSystemPrompt(cfg *config.Config) string {
	mem := ""
	if b, err := os.ReadFile(cfg.MemoryPath()); err == nil {
		mem = string(b)
	}
	s, err := prompt.Get(mem, memory.LatestSummary(cfg.Workspace))
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
