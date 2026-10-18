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
	"unicode"

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
			fatal("setup", err)
		}
	case "gateway":
		if err := runGateway(os.Args[2:]); err != nil {
			log.Fatalf("gateway: %v", err)
		}
	case "chat":
		if err := runChat(os.Args[2:]); err != nil {
			log.Fatalf("chat: %v", err)
		}
	case "fetch":
		if err := runFetch(os.Args[2:]); err != nil {
			fatal("fetch", err)
		}
	case "doctor":
		if err := runDoctor(os.Args[2:]); err != nil {
			fatal("doctor", err)
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
    Defaults come from the existing config.json when present, so Enter keeps
    the current setup; env vars still override them.
    Non-interactive env: TELEGRAM_BOT_TOKEN, PURU_BASE_URL, PURU_API_KEY,
    PURU_MODEL, PURU_WORKSPACE, PURU_ALLOWED_USERS, PURU_TEMPERATURE,
    PURU_RESTRICT_WORKSPACE, PURU_TIMEZONE, PURU_MAX_ITERATIONS,
    PURU_HISTORY_TOKEN_LIMIT, PURU_TOOLS_PREVIEW, PURU_LOOP_DELAY_SECONDS,
    PURU_EXEC_MEMORY_MB, PURU_SKILLS_MODE, PURU_SKILLS_ALLOW,
    PURU_HEALTH_HOST, PURU_HEALTH_PORT, PURU_WEBSEARCH_AISTUDIO_ACTIVE,
    PURU_WEBSEARCH_AISTUDIO_MODEL, PURU_WEBSEARCH_AISTUDIO_KEY,
    PURU_WEBSEARCH_EXA_ACTIVE, PURU_WEBSEARCH_EXA_KEY — every wizard field,
    so a GUI can drive the same setup. Add --force to overwrite without asking,
    --full for full setup without asking.

  puru gateway [--config PATH] [--health] [--host H] [--port P]
    Run the Telegram bot via long-polling. NO web server by default.
    Add --health to enable GET /health (Docker / VPS).

  puru chat "message..." [--config PATH] [--chat ID] [--reset]
    Local debug without Telegram (REPL when no args).

  puru fetch [-o FILE|-] URL [--max-bytes N] [--timeout SECONDS]
    Download a URL with the bot's own network stack. Use it on phones, where
    the shell has no curl and toybox wget cannot do HTTPS:
      puru fetch -o "$HOME/bin/tool" https://example.com/tool
    The file is downloaded to FILE.part and renamed, so a failed transfer
    never truncates an existing file. Executables (ELF / #!) get +x.

  puru doctor [--config PATH] [--offline]
    Print the environment report: config, workspace write/delete, HOME, TMPDIR,
    the shell the agent spawns, which tools are on PATH, DNS and HTTPS. Run it
    first when a build misbehaves. --offline skips the DNS/TLS checks.

Global flags:
  --config PATH   path to config.json (default ~/.puru/config.json)
  CONFIG env      inline JSON config, e.g. CONFIG='{"telegram_bot_token":"..."}'
                  (takes precedence over --config / file, for Docker/PaaS)

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

	nonInteractive := os.Getenv("PURU_NON_INTERACTIVE") != "" || !isTerminal(in)

	banner("PURU-AI setup", version+" · config: "+path)
	if _, err := os.Stat(path); err == nil && !o.force {
		warnf("config already exists: %s", path)
		if !askBool(in, "Overwrite it?", false) {
			hint("cancelled, existing config left unchanged")
			return nil
		}
	}
	// Defaults follow the current setup: env overrides, then the existing
	// config.json, then the built-in values. Pressing Enter therefore keeps
	// what is already configured, and the minimal wizard carries over every
	// field it does not ask. Every field has an env override (TELEGRAM_BOT_TOKEN,
	// PURU_BASE_URL, PURU_API_KEY, PURU_MODEL, PURU_WORKSPACE, PURU_ALLOWED_USERS,
	// PURU_TEMPERATURE, PURU_RESTRICT_WORKSPACE, PURU_TIMEZONE, PURU_MAX_ITERATIONS,
	// PURU_HISTORY_TOKEN_LIMIT, PURU_HEALTH_HOST, PURU_HEALTH_PORT,
	// PURU_TOOLS_PREVIEW, PURU_LOOP_DELAY_SECONDS, PURU_EXEC_MEMORY_MB,
	// PURU_SKILLS_MODE, PURU_SKILLS_ALLOW, PURU_WEBSEARCH_AISTUDIO_*,
	// PURU_WEBSEARCH_EXA_*), so a GUI or container can drive the same wizard.
	prev, hasPrev := loadPreviousConfig(path)
	if hasPrev {
		if nonInteractive {
			hint("non-interactive: no questions asked, values kept from %s", path)
		} else {
			hint("defaults come from the existing config; Enter keeps the current value")
		}
	} else if nonInteractive {
		hint("non-interactive: env vars and defaults, no questions asked")
	}

	token := firstNonEmpty(os.Getenv("TELEGRAM_BOT_TOKEN"), prev.TelegramBotToken)
	baseURL := firstNonEmpty(os.Getenv("PURU_BASE_URL"), prev.Model.BaseURL, "https://api.openai.com/v1")
	apiKey := firstNonEmpty(os.Getenv("PURU_API_KEY"), prev.Model.APIKey)
	model := firstNonEmpty(os.Getenv("PURU_MODEL"), prev.Model.Model, "gpt-4o-mini")
	workspace := firstNonEmpty(os.Getenv("PURU_WORKSPACE"), prev.Workspace, filepath.Join(config.DefaultDir(), "workspace"))

	allowedUsers := prev.TelegramAllowedUsers
	if allowedUsers == nil {
		allowedUsers = []int64{}
	}
	allowedUsers = envOrInt64List("PURU_ALLOWED_USERS", allowedUsers)
	temperature := envOrFloat("PURU_TEMPERATURE", prev.Model.Temperature)
	restrictWorkspace := envOrBool("PURU_RESTRICT_WORKSPACE", prev.restrictWorkspaceDefault())
	maxIterations := envOrInt("PURU_MAX_ITERATIONS", firstNonZero(prev.MaxIterations, config.DefaultMaxIterations))
	historyTokenLimit := envOrInt("PURU_HISTORY_TOKEN_LIMIT", firstNonZero(prev.HistoryTokenLimit, config.DefaultHistoryTokLimit))
	host := envOrString("PURU_HEALTH_HOST", firstNonEmpty(prev.Host, config.DefaultHealthHost))
	port := envOrInt("PURU_HEALTH_PORT", firstNonZero(prev.Port, config.DefaultHealthPort))
	toolsPreview := envOrBool("PURU_TOOLS_PREVIEW", prev.ShowToolsPreview())
	loopDelay := envOrInt("PURU_LOOP_DELAY_SECONDS", firstNonZero(prev.LoopDelaySeconds, config.DefaultLoopDelaySeconds))
	execMemory := envOrInt("PURU_EXEC_MEMORY_MB", firstNonZero(prev.ExecMemoryMB, config.DefaultExecMemoryMB))
	skillsMode := envOrString("PURU_SKILLS_MODE", firstNonEmpty(prev.SkillsMode, "default"))
	skillsAllow := prev.SkillsAllow
	if skillsAllow == nil {
		skillsAllow = []string{}
	}
	skillsAllow = envOrList("PURU_SKILLS_ALLOW", skillsAllow)
	timezone := envOrString("PURU_TIMEZONE", firstNonEmpty(prev.Timezone, config.DefaultTimezone))
	webActive := envOrBool("PURU_WEBSEARCH_AISTUDIO_ACTIVE", prev.WebSearch.AIStudio.Active)
	webModel := envOrString("PURU_WEBSEARCH_AISTUDIO_MODEL", firstNonEmpty(prev.WebSearch.AIStudio.Model, "gemini-flash-lite-latest"))
	webAPIKey := envOrString("PURU_WEBSEARCH_AISTUDIO_KEY", prev.WebSearch.AIStudio.APIKey)
	exaActive := envOrBool("PURU_WEBSEARCH_EXA_ACTIVE", prev.WebSearch.Exa.Active)
	exaAPIKey := envOrString("PURU_WEBSEARCH_EXA_KEY", prev.WebSearch.Exa.APIKey)

	full := o.full
	if !nonInteractive {
		if o.full {
			hint("--full: every field is asked, Enter keeps the default")
		} else {
			full = askBool(in, "Full setup (ask every field)?", true)
			if full {
				hint("full: every field is asked, Enter keeps the default")
			} else {
				hint("minimal: required fields only, defaults for the rest")
			}
		}

		section("Telegram")
		token = askLine(in, "Bot token (from @BotFather)", token, true)
		if full {
			allowedUsers = askInt64List(in, "Allowed user IDs (comma-separated, empty = everyone allowed)", allowedUsers)
		}

		section("Model (OpenAI-compatible)")
		baseURL = askLine(in, "Base URL", baseURL, true)
		apiKey = askLine(in, "API key", apiKey, false)
		model = askLine(in, "Model name", model, true)
		if full {
			temperature = askFloat(in, "Temperature (0 = provider default)", temperature)
		}

		section("Workspace")
		workspace = askLine(in, "Workspace directory", workspace, true)
		if full {
			restrictWorkspace = askBool(in, "Restrict file/exec access to the workspace?", restrictWorkspace)
		}

		if full {
			section("Runtime")
			maxIterations = askInt(in, "Max iterations per message", maxIterations)
			historyTokenLimit = askInt(in, "History token limit (compaction trigger)", historyTokenLimit)
			toolsPreview = askBool(in, "Tools preview (live tool calls)", toolsPreview)
			loopDelay = askInt(in, "Loop delay seconds", loopDelay)
			execMemory = askInt(in, "Exec memory MB (min 64)", execMemory)
			timezone = askLine(in, "Timezone (IANA, e.g. Asia/Jakarta)", timezone, false)

			section("Health server")
			host = askLine(in, "Host", host, false)
			port = askInt(in, "Port", port)

			section("Skills")
			skillsMode = askSkillsMode(in, skillsMode)
			skillsAllow = askStringList(in, "Allowlist (used when skills_mode=custom)", skillsAllow)

			section("Web search")
			webActive = askBool(in, "AI Studio (Gemini grounding) active?", webActive)
			webModel = askLine(in, "AI Studio model", webModel, false)
			webAPIKey = askLine(in, "AI Studio API key (empty = disabled)", webAPIKey, false)
			exaActive = askBool(in, "Exa active?", exaActive)
			exaAPIKey = askLine(in, "Exa API key (empty = disabled)", exaAPIKey, false)
		}
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
			"exa": map[string]any{
				"active":  exaActive,
				"api_key": strings.TrimSpace(exaAPIKey),
			},
		},
	}
	// Preview what will be written; advanced fields appear in full setup only.
	rows := [][2]string{
		{"telegram token", maskSecret(token)},
		{"telegram users", userIDsText(allowedUsers)},
		{"model", strings.TrimSpace(model)},
		{"base_url", strings.TrimSpace(baseURL)},
		{"api_key", maskSecret(apiKey)},
		{"workspace", strings.TrimSpace(workspace)},
	}
	if full {
		rows = append(rows,
			[2]string{"restrict_workspace", boolText(restrictWorkspace)},
			[2]string{"max_iterations", strconv.Itoa(maxIterations)},
			[2]string{"history_token_limit", strconv.Itoa(historyTokenLimit)},
			[2]string{"tools_preview", boolText(toolsPreview)},
			[2]string{"loop_delay_seconds", strconv.Itoa(loopDelay)},
			[2]string{"exec_memory_mb", strconv.Itoa(execMemory)},
			[2]string{"temperature", strconv.FormatFloat(temperature, 'g', -1, 64)},
			[2]string{"timezone", strings.TrimSpace(timezone)},
			[2]string{"health", strings.TrimSpace(host) + ":" + strconv.Itoa(port)},
			[2]string{"skills_mode", skillsMode},
			[2]string{"skills_allow", listText(skillsAllow)},
			[2]string{"web aistudio", boolText(webActive) + " · " + webModel + " · key " + secretText(webAPIKey)},
			[2]string{"web exa", boolText(exaActive) + " · key " + secretText(exaAPIKey)},
		)
	}
	summary("Configuration", rows)

	if err := writeConfigJSON(path, cfg); err != nil {
		return err
	}
	// Validate by loading (also creates workspace + history dirs).
	// CONFIG env takes precedence in Load, so unset it briefly to
	// validate the file we just wrote, then restore.
	if inline, hadCONFIG := os.LookupEnv("CONFIG"); hadCONFIG {
		_ = os.Unsetenv("CONFIG")
		defer func() { _ = os.Setenv("CONFIG", inline) }()
	}
	if _, err := config.Load(path); err != nil {
		return fmt.Errorf("config written but invalid: %w", err)
	}
	next := "puru gateway"
	if path != config.DefaultPath() {
		next = "puru gateway --config " + path
	}
	done("Config saved: "+path, next)
	return nil
}

// previousConfig holds the values of an existing config.json, used as wizard
// defaults so re-running setup starts from the current setup.
type previousConfig struct {
	config.Config
	// restrictWorkspaceSet separates an absent key (default on) from an
	// explicit false, which binary unmarshalling alone cannot tell apart.
	restrictWorkspaceSet bool
}

// loadPreviousConfig reads path and reports whether values were loaded. A
// missing, unreadable, or invalid file yields zero values: the wizard is about
// to rewrite it, so it continues with built-in defaults.
func loadPreviousConfig(path string) (*previousConfig, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return &previousConfig{}, false
	}
	prev := &previousConfig{}
	if err := json.Unmarshal(raw, prev); err != nil {
		warnf("ignoring existing %s: %v", path, err)
		return prev, false
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err == nil {
		_, prev.restrictWorkspaceSet = keys["restrict_workspace"]
	}
	return prev, true
}

// restrictWorkspaceDefault reports the stored workspace jail setting; an absent
// key means on, the same default config.Load applies.
func (p *previousConfig) restrictWorkspaceDefault() bool {
	if !p.restrictWorkspaceSet {
		return true
	}
	return p.RestrictWorkspace
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

// envString returns the trimmed value of name, reporting false when unset.
func envString(name string) (string, bool) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", false
	}
	return value, true
}

// envInt parses name as an int; unset or unparsable values report false, so the
// caller keeps its previous value.
func envInt(name string) (int, bool) {
	value, ok := envString(name)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		warnf("ignoring %s=%q: not a number", name, value)
		return 0, false
	}
	return n, true
}

// envFloat parses name as a float. Zero is a meaningful value (temperature),
// so the caller must rely on the boolean, never on the value.
func envFloat(name string) (float64, bool) {
	value, ok := envString(name)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		warnf("ignoring %s=%q: not a number", name, value)
		return 0, false
	}
	return f, true
}

// envBool parses name as a boolean: 1/0, true/false, yes/no, on/off.
func envBool(name string) (bool, bool) {
	value, ok := envString(name)
	if !ok {
		return false, false
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "y", "on":
		return true, true
	case "0", "false", "no", "n", "off":
		return false, true
	}
	warnf("ignoring %s=%q: not a boolean", name, value)
	return false, false
}

// splitList splits a comma or whitespace separated environment value.
func splitList(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if field = strings.TrimSpace(field); field != "" {
			out = append(out, field)
		}
	}
	return out
}

// envList parses name as a comma/whitespace separated string list.
func envList(name string) ([]string, bool) {
	value, ok := envString(name)
	if !ok {
		return nil, false
	}
	return splitList(value), true
}

// envInt64List parses name as a comma/whitespace separated integer list, e.g.
// the Telegram user ID allowlist.
func envInt64List(name string) ([]int64, bool) {
	value, ok := envString(name)
	if !ok {
		return nil, false
	}
	fields := splitList(value)
	ids := make([]int64, 0, len(fields))
	for _, field := range fields {
		id, err := strconv.ParseInt(field, 10, 64)
		if err != nil {
			warnf("ignoring %s=%q: not an integer list", name, value)
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

func envOrString(name, fallback string) string {
	if value, ok := envString(name); ok {
		return value
	}
	return fallback
}

func envOrInt(name string, fallback int) int {
	if value, ok := envInt(name); ok {
		return value
	}
	return fallback
}

func envOrFloat(name string, fallback float64) float64 {
	if value, ok := envFloat(name); ok {
		return value
	}
	return fallback
}

func envOrBool(name string, fallback bool) bool {
	if value, ok := envBool(name); ok {
		return value
	}
	return fallback
}

func envOrList(name string, fallback []string) []string {
	if value, ok := envList(name); ok {
		return value
	}
	return fallback
}

func envOrInt64List(name string, fallback []int64) []int64 {
	if value, ok := envInt64List(name); ok {
		return value
	}
	return fallback
}

// firstNonZero returns v, or fallback when v is zero or negative — the
// "unset" sentinel config.Load uses for the optional numeric fields.
func firstNonZero(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
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
			stored = messages.KeepLastExchange(stored)
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
