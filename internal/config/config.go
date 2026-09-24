// Package config loads PURU-AI settings from a single JSON file.
//
// Default location: $HOME/.puru/config.json (for root: /root/.puru/config.json).
// Override with --config flag or PURU_CONFIG env. No .env, no web UI.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/purujawa06-bot/PURU-AI/internal/workspace"
)

const (
	DefaultMaxIterations   = 500
	DefaultHistoryTokLimit = 30000
	DefaultExecTimeoutSec  = 60
	MaxExecTimeoutSec      = 300
	// DefaultHealthHost/Port: health check HTTP saja (GET /healthz).
	DefaultHealthHost = "0.0.0.0"
	DefaultHealthPort = 8080
	// DefaultToolsPreview: tampilkan tools yang dipakai AI secara live.
	DefaultToolsPreview = true
	// DefaultLoopDelaySeconds: jeda antar loop/iterasi agent.
	DefaultLoopDelaySeconds = 3
	MaxLoopDelaySeconds     = 60
	// DefaultExecMemoryMB: budget RAM grup proses exec (default = minimal 64MB).
	// Lebih dari ini → grup proses di-kill. Wajib di VPS kecil.
	DefaultExecMemoryMB = 64
	MinExecMemoryMB     = 64
)

// ModelConfig is the single OpenAI-compatible endpoint. No fallback,
// no per-user override, no relay.
type ModelConfig struct {
	BaseURL     string  `json:"base_url"`
	APIKey      string  `json:"api_key"`
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
}

type Config struct {
	TelegramBotToken string `json:"telegram_bot_token"`
	// TelegramAllowedUsers: allowlist ID user Telegram. Kosong = semua boleh.
	TelegramAllowedUsers []int64     `json:"telegram_allowed_users"`
	Model                ModelConfig `json:"model"`
	Workspace            string      `json:"workspace"`
	// RestrictWorkspace jails the agent inside Workspace: file tools reject
	// absolute paths / ../ escapes outside it, and exec runs with Dir forced
	// inside it.
	RestrictWorkspace bool `json:"restrict_workspace"`
	MaxIterations     int  `json:"max_iterations"`
	HistoryTokenLimit int  `json:"history_token_limit"`
	// Host/Port hanya untuk health check HTTP (GET /healthz).
	Host string `json:"host"`
	Port int    `json:"port"`
	// ToolsPreview: bila true (default), bot menampilkan live tools apa yang
	// dipakai AI via edit message. Pointer agar "tidak diisi" = true.
	ToolsPreview *bool `json:"tools_preview"`
	// LoopDelaySeconds: jeda antar loop/iterasi agent (default 3, maks 60).
	LoopDelaySeconds int `json:"loop_delay_seconds"`
	// ExecMemoryMB: budget RAM untuk tiap perintah exec (default = min 64).
	// Grup proses yang lewat budget langsung di-kill (linux).
	ExecMemoryMB int    `json:"exec_memory_mb"`
	ConfigDir    string `json:"-"`
}

// DefaultDir returns $HOME/.puru (/root/.puru for root).
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "/root"
	}
	return filepath.Join(home, ".puru")
}

// DefaultPath returns the default config.json path.
func DefaultPath() string { return filepath.Join(DefaultDir(), "config.json") }

// ResolvePath applies flag > env > default precedence.
func ResolvePath(flagPath string) string {
	if flagPath != "" {
		return flagPath
	}
	if v := os.Getenv("PURU_CONFIG"); v != "" {
		return v
	}
	return DefaultPath()
}

// Load reads path (or the default when empty), applies defaults, validates,
// and ensures workspace + history dirs exist. Fast: single small JSON read.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath()
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w (copy from example.config.json)", path, err)
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("config %s is not valid JSON: %w", path, err)
	}
	c.ConfigDir = filepath.Dir(path)

	if c.TelegramBotToken == "" {
		return nil, fmt.Errorf("config %s: telegram_bot_token is required", path)
	}
	if c.Model.BaseURL == "" {
		return nil, fmt.Errorf("config %s: model.base_url is required", path)
	}
	if c.Model.Model == "" {
		return nil, fmt.Errorf("config %s: model.model is required", path)
	}
	if c.MaxIterations <= 0 {
		c.MaxIterations = DefaultMaxIterations
	}
	if c.HistoryTokenLimit <= 0 {
		c.HistoryTokenLimit = DefaultHistoryTokLimit
	}
	if c.Host == "" {
		c.Host = DefaultHealthHost
	}
	if c.Port <= 0 {
		c.Port = DefaultHealthPort
	}
	if c.LoopDelaySeconds <= 0 {
		c.LoopDelaySeconds = DefaultLoopDelaySeconds
	}
	if c.LoopDelaySeconds > MaxLoopDelaySeconds {
		c.LoopDelaySeconds = MaxLoopDelaySeconds
	}
	if c.ExecMemoryMB <= 0 {
		c.ExecMemoryMB = DefaultExecMemoryMB
	}
	if c.ExecMemoryMB < MinExecMemoryMB {
		c.ExecMemoryMB = MinExecMemoryMB
	}
	if c.Workspace == "" {
		c.Workspace = filepath.Join(DefaultDir(), "workspace")
	}
	abs, err := filepath.Abs(c.Workspace)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace: %w", err)
	}
	c.Workspace = abs
	if err := workspace.Ensure(c.Workspace); err != nil {
		return nil, fmt.Errorf("create workspace %s: %w", c.Workspace, err)
	}
	if err := os.MkdirAll(filepath.Join(DefaultDir(), "history"), 0o755); err != nil {
		return nil, fmt.Errorf("create history dir: %w", err)
	}
	return &c, nil
}

// IsUserAllowed reports whether a Telegram user id may use the bot.
// Empty TelegramAllowedUsers = everyone allowed.
func (c *Config) IsUserAllowed(id int64) bool {
	if c == nil || len(c.TelegramAllowedUsers) == 0 {
		return true
	}
	for _, a := range c.TelegramAllowedUsers {
		if a == id {
			return true
		}
	}
	return false
}

// ShowToolsPreview reports whether live tool-call preview is enabled
// (default true when unset).
func (c *Config) ShowToolsPreview() bool {
	if c == nil || c.ToolsPreview == nil {
		return DefaultToolsPreview
	}
	return *c.ToolsPreview
}

// LoopDelay is the pause between agent iterations.
func (c *Config) LoopDelay() time.Duration {
	if c == nil || c.LoopDelaySeconds <= 0 {
		return time.Duration(DefaultLoopDelaySeconds) * time.Second
	}
	return time.Duration(c.LoopDelaySeconds) * time.Second
}

// MemoryPath is <workspace>/memory/MEMORY.md — single memory file, local.
func (c *Config) MemoryPath() string { return workspace.MemoryPath(c.Workspace) }

// MemoryDir is <workspace>/memory (MEMORY.md plus context/ summaries).
func (c *Config) MemoryDir() string { return workspace.MemoryDir(c.Workspace) }

// ContextDir is <workspace>/memory/context (system-managed summaries).
func (c *Config) ContextDir() string { return workspace.ContextDir(c.Workspace) }

// SkillsDir is <workspace>/skills (one SKILL.md per installed skill).
func (c *Config) SkillsDir() string { return workspace.SkillsDir(c.Workspace) }

// HistoryDir is ~/.puru/history (per-chat JSON files).
func (c *Config) HistoryDir() string { return filepath.Join(DefaultDir(), "history") }
