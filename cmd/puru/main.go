// Command puru is the npm-friendly CLI for PURU-AI (no web server by default).
//
// Usage:
//
//	puru setup [--config PATH] [--force]   — interactive wizard, writes config.json
//	puru gateway [--config PATH] [--health] — run Telegram bot (no /health unless --health)
//	puru chat "message..." [--config PATH] [--chat ID] [--reset] — local debug, no Telegram
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
	"strconv"
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
	"github.com/purujawa06-bot/PURU-AI/internal/schedule"
	"github.com/purujawa06-bot/PURU-AI/internal/telegram"
)

// version is injected at release time: -ldflags="-X main.version=v1.2.3".
var version = "dev"

func main() {
	debug.SetMemoryLimit(50 << 20)

	// Reap orphaned exec grandchildren (PID 1 zombie collector, unix only).
	ai.StartReaper()

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
	fmt.Printf(`puru %s — PURU-AI CLI (no web server unless opted in)

Usage:
  puru setup [--config PATH] [--force] [--full]
    Interactive wizard, writes config.json (default %s).
    First question: full setup? [Y/n] (default Y = full).
    - Y (full, default): asks every field in example.config.json,
      even ones that already have defaults (Enter = keep default).
    - N (minimal): only required fields
      (telegram token, model base_url/api_key/name, workspace).
    Non-interactive env: TELEGRAM_BOT_TOKEN, PURU_BASE_URL, PURU_API_KEY,
    PURU_MODEL, PURU_WORKSPACE. Add --force to overwrite without asking,
    --full for full setup without asking.

  puru gateway [--config PATH] [--health] [--host H] [--port P]
    Run the Telegram bot via long-polling. NO web server by default.
    Add --health to enable GET /health (Docker / VPS).

  puru chat "message..." [--config PATH] [--chat ID] [--reset]
    Local debug without Telegram (REPL when no args).

Global flags:
  --config PATH   path to config.json (default ~/.puru/config.json)

Examples:
  puru setup
  puru gateway --config ~/.puru/config.json
  puru gateway --health --port 8080
`, version, config.DefaultPath())
}

// ---------- setup ----------

type setupOptions struct {
	configPath string
	force      bool
	full       bool
}

func parseSetupArgs(args []string) (*setupOptions, error) {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	o := &setupOptions{}
	fs.StringVar(&o.configPath, "config", "", "path to config.json")
	fs.BoolVar(&o.force, "force", false, "overwrite config without asking")
	fs.BoolVar(&o.full, "full", false, "full setup: ask all fields, skip minimal prompt")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected args: %s", strings.Join(fs.Args(), " "))
	}
	return o, nil
}

func runSetup(args []string) error {
	o, err := parseSetupArgs(args)
	if err != nil {
		return err
	}
	path := config.ResolvePath(o.configPath)
	in := bufio.NewReader(os.Stdin)
	if _, err := os.Stat(path); err == nil && !o.force {
		if !askYesFromReader(in, fmt.Sprintf("Config %s already exists. Overwrite? [y/N]: ", path), false) {
			fmt.Println("Cancelled, config left unchanged.")
			return nil
		}
	}
	nonInteractive := os.Getenv("PURU_NON_INTERACTIVE") != "" || !isTerminal(in)

	token := firstNonEmpty(os.Getenv("TELEGRAM_BOT_TOKEN"), "")
	baseURL := firstNonEmpty(os.Getenv("PURU_BASE_URL"), "https://api.openai.com/v1")
	apiKey := firstNonEmpty(os.Getenv("PURU_API_KEY"), "")
	model := firstNonEmpty(os.Getenv("PURU_MODEL"), "gpt-4o-mini")
	workspace := firstNonEmpty(os.Getenv("PURU_WORKSPACE"), filepath.Join(config.DefaultDir(), "workspace"))

	// Defaults for the rest (same as example.config.json / config package).
	allowedUsers := []int64{}
	temperature := 0.0
	restrictWorkspace := true
	maxIterations := config.DefaultMaxIterations
	historyTokenLimit := config.DefaultHistoryTokLimit
	host := config.DefaultHealthHost
	port := config.DefaultHealthPort
	toolsPreview := true
	loopDelay := config.DefaultLoopDelaySeconds
	execMemory := config.DefaultExecMemoryMB
	skillsMode := "default"
	skillsAllow := []string{}
	timezone := config.DefaultTimezone
	webActive := false
	webModel := "gemini-flash-lite-latest"
	webAPIKey := ""

	full := o.full
	if !nonInteractive && !o.full {
		fmt.Println("== PURU-AI setup ==")
		full = askYesFromReader(in, "Full setup (ask all fields)? [Y/n]: ", true)
		if full {
			fmt.Println("-- full setup: all fields, Enter = keep default --")
		} else {
			fmt.Println("-- minimal setup: required fields only, rest use defaults --")
		}
		token = askLine(in, "Telegram bot token (from @BotFather)", token, true)
		baseURL = askLine(in, "Model base_url (OpenAI-compatible)", baseURL, true)
		apiKey = askLine(in, "Model api_key", apiKey, true)
		model = askLine(in, "Model name", model, true)
		workspace = askLine(in, "Workspace dir", workspace, true)
	} else if !nonInteractive && o.full {
		fmt.Println("== PURU-AI setup (full) ==")
		token = askLine(in, "Telegram bot token (from @BotFather)", token, true)
		baseURL = askLine(in, "Model base_url (OpenAI-compatible)", baseURL, true)
		apiKey = askLine(in, "Model api_key", apiKey, true)
		model = askLine(in, "Model name", model, true)
		workspace = askLine(in, "Workspace dir", workspace, true)
	} else if nonInteractive {
		// env-only, minimal defaults; --full has no extra effect without TTY.
		full = o.full
	}
	if !nonInteractive && full {
		allowedUsers = askInt64List(in, "Telegram allowed user IDs (comma-separated, empty = all allowed)", allowedUsers)
		temperature = askFloat(in, "Model temperature", temperature)
		restrictWorkspace = askBool(in, "Restrict workspace (jail files/exec inside workspace)", restrictWorkspace)
		maxIterations = askInt(in, "Max iterations", maxIterations)
		historyTokenLimit = askInt(in, "History token limit", historyTokenLimit)
		host = askLine(in, "Health host", host, false)
		port = askInt(in, "Health port", port)
		toolsPreview = askBool(in, "Tools preview (live tool calls)", toolsPreview)
		loopDelay = askInt(in, "Loop delay seconds", loopDelay)
		execMemory = askInt(in, "Exec memory MB (min 64)", execMemory)
		skillsMode = askSkillsMode(in, skillsMode)
		skillsAllow = askStringList(in, "Skills allowlist (comma-separated, used when skills_mode=custom)", skillsAllow)
		timezone = askLine(in, "Timezone (IANA, e.g. Asia/Jakarta)", timezone, false)
		webActive = askBool(in, "Web search (aistudio) active", webActive)
		webModel = askLine(in, "Web search model", webModel, false)
		webAPIKey = askLine(in, "Web search api_key (empty = disabled)", webAPIKey, false)
	}
	if strings.TrimSpace(token) == "" {
		return errors.New("telegram_bot_token is required (env TELEGRAM_BOT_TOKEN)")
	}
	if strings.TrimSpace(baseURL) == "" {
		return errors.New("model.base_url is required (env PURU_BASE_URL)")
	}
	if strings.TrimSpace(model) == "" {
		return errors.New("model.model is required (env PURU_MODEL)")
	}

	cfg := map[string]any{
		"telegram_bot_token":     strings.TrimSpace(token),
		"telegram_allowed_users": allowedUsers,
		"model": map[string]any{
			"base_url":    strings.TrimSpace(baseURL),
			"api_key":     strings.TrimSpace(apiKey),
			"model":       strings.TrimSpace(model),
			"temperature": temperature,
		},
		"workspace":           strings.TrimSpace(workspace),
		"restrict_workspace":  restrictWorkspace,
		"max_iterations":      maxIterations,
		"history_token_limit": historyTokenLimit,
		"host":                strings.TrimSpace(host),
		"port":                port,
		"tools_preview":       toolsPreview,
		"loop_delay_seconds":  loopDelay,
		"exec_memory_mb":      execMemory,
		"skills_mode":         strings.TrimSpace(skillsMode),
		"skills_allow":        skillsAllow,
		"timezone":            strings.TrimSpace(timezone),
		"web_search": map[string]any{
			"aistudio": map[string]any{
				"active":  webActive,
				"model":   strings.TrimSpace(webModel),
				"api_key": strings.TrimSpace(webAPIKey),
			},
		},
	}
	if err := writeConfigJSON(path, cfg); err != nil {
		return err
	}
	// Validate by loading (also creates workspace + history dirs).
	if _, err := config.Load(path); err != nil {
		return fmt.Errorf("config written but invalid: %w", err)
	}
	fmt.Printf("OK config saved: %s\n", path)
	fmt.Println("Next: puru gateway --config " + path)
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

func askYesFromReader(in *bufio.Reader, label string, def bool) bool {
	fmt.Print(label)
	line, _ := in.ReadString('\n')
	return normalizeYes(line, def)
}

func askBool(in *bufio.Reader, label string, def bool) bool {
	defStr := "y/N"
	if def {
		defStr = "Y/n"
	}
	fmt.Printf("%s [%s]: ", label, defStr)
	line, _ := in.ReadString('\n')
	return normalizeYes(line, def)
}

func askInt(in *bufio.Reader, label string, def int) int {
	fmt.Printf("%s [%d]: ", label, def)
	line, _ := in.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	n, err := strconv.Atoi(line)
	if err != nil {
		fmt.Printf("Invalid number, keeping default %d.\n", def)
		return def
	}
	return n
}

func askFloat(in *bufio.Reader, label string, def float64) float64 {
	fmt.Printf("%s [%g]: ", label, def)
	line, _ := in.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	f, err := strconv.ParseFloat(line, 64)
	if err != nil {
		fmt.Printf("Invalid number, keeping default %g.\n", def)
		return def
	}
	return f
}

func askInt64List(in *bufio.Reader, label string, def []int64) []int64 {
	defStr := ""
	if len(def) > 0 {
		parts := make([]string, len(def))
		for i, v := range def {
			parts[i] = strconv.FormatInt(v, 10)
		}
		defStr = strings.Join(parts, ",")
	}
	fmt.Printf("%s [%s]: ", label, defStr)
	line, _ := in.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	var out []int64
	for _, p := range strings.Split(line, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			fmt.Printf("Skipping invalid id %q.\n", p)
			continue
		}
		out = append(out, n)
	}
	if out == nil {
		return []int64{}
	}
	return out
}

func askStringList(in *bufio.Reader, label string, def []string) []string {
	fmt.Printf("%s [%s]: ", label, strings.Join(def, ","))
	line, _ := in.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	var out []string
	for _, p := range strings.Split(line, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}

func askSkillsMode(in *bufio.Reader, def string) string {
	fmt.Printf("Skills mode (default/off/custom) [%s]: ", def)
	line, _ := in.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "" {
		return def
	}
	switch line {
	case "default", "off", "custom":
		return line
	default:
		fmt.Printf("Unknown skills_mode, keeping default %s.\n", def)
		return def
	}
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

// ---------- gateway (no web server by default) ----------

type gatewayOptions struct {
	configPath string
	withHealth bool
	host       string
	port       int
}

func parseGatewayArgs(args []string) (*gatewayOptions, error) {
	fs := flag.NewFlagSet("gateway", flag.ContinueOnError)
	o := &gatewayOptions{}
	fs.StringVar(&o.configPath, "config", "", "path to config.json")
	fs.BoolVar(&o.withHealth, "health", false, "enable GET /health")
	fs.StringVar(&o.host, "host", "", "override health host (default from config)")
	fs.IntVar(&o.port, "port", 0, "override health port (default from config)")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected args: %s", strings.Join(fs.Args(), " "))
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

	// Scheduled tasks runner (Picoclaw cron-like, default Asia/Jakarta).
	go (&schedule.Runner{
		Store: schedule.NewStore(cfg.Workspace),
		Handle: func(runCtx context.Context, job schedule.Job) error {
			return appSvc.RunScheduledJob(runCtx, job)
		},
	}).Start(ctx)

	// Health check only with --health. CLI/npm default has no web server.
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

	return app.PollUpdates(ctx, tg, appSvc.Handle)
}

// ---------- chat (local debug, no Telegram) ----------

type chatOptions struct {
	configPath string
	chatID     int64
	reset      bool
}

func parseChatArgs(args []string) (*chatOptions, []string, error) {
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	o := &chatOptions{}
	fs.StringVar(&o.configPath, "config", "", "path to config.json")
	fs.Int64Var(&o.chatID, "chat", -777, "debug chat id")
	fs.BoolVar(&o.reset, "reset", false, "clear history and exit")
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
		fmt.Printf("CLI PURU-AI — chat=%d — /exit to quit, /reset to clear history\n", o.chatID)
		sc := bufio.NewScanner(os.Stdin)
		for {
			fmt.Print("You > ")
			if !sc.Scan() {
				return nil
			}
			line := strings.TrimSpace(sc.Text())
			if line == "/exit" || line == "/quit" {
				return nil
			}
			if line == "/reset" {
				_ = hist.Clear(o.chatID)
				fmt.Println("History cleared.")
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
			fmt.Printf("(saved: %s)\n", rel)
		}
	}
	opts := &ai.ProcessOptions{ChatID: chatID, Channel: "cli"}
	if cfg.ShowToolsPreview() {
		opts.OnTool = func(name string, args map[string]any) {
			fmt.Printf("🔧 %s\n", name)
		}
	}
	res := agent.ProcessMessage(ctx, p, stored, opts)
	saved := append(append([]*messages.Message{}, stored...), userMsg(p)...)
	// Keep as-is; no prune — compaction handles it.
	saved = append(saved, messages.SanitizeHistoryMessages(res.ResponseMessages)...)
	_ = hist.Set(chatID, saved)
	return res.Text
}

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
