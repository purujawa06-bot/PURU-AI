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
)

const (
	DefaultMaxIterations   = 500
	DefaultHistoryTokLimit = 30000
	DefaultExecTimeoutSec  = 60
	MaxExecTimeoutSec      = 300
	// DefaultHealthHost/Port: health check HTTP saja (GET /healthz).
	DefaultHealthHost = "0.0.0.0"
	DefaultHealthPort = 8080
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
	TelegramBotToken string      `json:"telegram_bot_token"`
	Model            ModelConfig `json:"model"`
	Workspace        string      `json:"workspace"`
	// RestrictWorkspace jails the agent inside Workspace: file tools reject
	// absolute paths / ../ escapes outside it, and exec runs with Dir forced
	// inside it.
	RestrictWorkspace bool `json:"restrict_workspace"`
	MaxIterations     int  `json:"max_iterations"`
	HistoryTokenLimit int  `json:"history_token_limit"`
	// Host/Port hanya untuk health check HTTP (GET /healthz).
	Host      string `json:"host"`
	Port      int    `json:"port"`
	ConfigDir string `json:"-"`
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
		return nil, fmt.Errorf("baca config %s: %w (salin dari example.config.json)", path, err)
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("config %s bukan JSON valid: %w", path, err)
	}
	c.ConfigDir = filepath.Dir(path)

	if c.TelegramBotToken == "" {
		return nil, fmt.Errorf("config %s: telegram_bot_token wajib diisi", path)
	}
	if c.Model.BaseURL == "" {
		return nil, fmt.Errorf("config %s: model.base_url wajib diisi", path)
	}
	if c.Model.Model == "" {
		return nil, fmt.Errorf("config %s: model.model wajib diisi", path)
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
	if c.Workspace == "" {
		c.Workspace = filepath.Join(DefaultDir(), "workspace")
	}
	abs, err := filepath.Abs(c.Workspace)
	if err != nil {
		return nil, fmt.Errorf("workspace tidak valid: %w", err)
	}
	c.Workspace = abs
	if err := os.MkdirAll(c.Workspace, 0o755); err != nil {
		return nil, fmt.Errorf("buat workspace %s: %w", c.Workspace, err)
	}
	if err := os.MkdirAll(filepath.Join(DefaultDir(), "history"), 0o755); err != nil {
		return nil, fmt.Errorf("buat history dir: %w", err)
	}
	return &c, nil
}

// MemoryPath is <workspace>/MEMORY.md — single memory file, local.
func (c *Config) MemoryPath() string { return filepath.Join(c.Workspace, "MEMORY.md") }

// HistoryDir is ~/.puru/history (per-chat JSON files).
func (c *Config) HistoryDir() string { return filepath.Join(DefaultDir(), "history") }
