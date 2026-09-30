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
	"strings"
	"time"

	"github.com/purujawa06-bot/PURU-AI/internal/workspace"
)

const (
	DefaultMaxIterations   = 500
	DefaultHistoryTokLimit = 30000
	DefaultExecTimeoutSec  = 60
	MaxExecTimeoutSec      = 300
	// DefaultHealthHost/Port: HTTP health check only (GET /health).
	DefaultHealthHost = "0.0.0.0"
	DefaultHealthPort = 8080
	// DefaultToolsPreview: show live tool calls via message edits.
	DefaultToolsPreview = true
	// DefaultLoopDelaySeconds: pause between agent loop iterations.
	DefaultLoopDelaySeconds = 3
	MaxLoopDelaySeconds     = 60
	// DefaultExecMemoryMB: RAM budget per exec process group (default = minimum 64MB).
	// Over-budget groups are killed. Required on small VPS.
	DefaultExecMemoryMB = 64
	MinExecMemoryMB     = 64
	// DefaultTimezone is the IANA name used for wall-clock schedules.
	// Jobs may override it per job; empty config falls back here.
	DefaultTimezone = "Asia/Jakarta"
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
	// TelegramAllowedUsers: Telegram user ID allowlist. Empty = everyone allowed.
	TelegramAllowedUsers []int64     `json:"telegram_allowed_users"`
	Model                ModelConfig `json:"model"`
	Workspace            string      `json:"workspace"`
	// RestrictWorkspace jails the agent inside Workspace: file tools reject
	// absolute paths / ../ escapes outside it, and exec runs with Dir forced
	// inside it.
	RestrictWorkspace bool `json:"restrict_workspace"`
	MaxIterations     int  `json:"max_iterations"`
	HistoryTokenLimit int  `json:"history_token_limit"`
	// Host/Port for the HTTP health check only (GET /health).
	Host string `json:"host"`
	Port int    `json:"port"`
	// ToolsPreview: when true (default), the bot live-displays tool calls
	// via message edits. Pointer so unset means true.
	ToolsPreview *bool `json:"tools_preview"`
	// LoopDelaySeconds: pause between agent loop iterations (default 3, max 60).
	LoopDelaySeconds int `json:"loop_delay_seconds"`
	// ExecMemoryMB: RAM budget per exec command (default = min 64).
	// Over-budget process groups are killed (linux).
	ExecMemoryMB int `json:"exec_memory_mb"`
	// SkillsMode controls skill injection into the system prompt,
	// picoclaw turn_profile.skills-like: "" or "default" = full catalog +
	// frontmatter active skills; "off" = no skills in prompt; "custom" =
	// only SkillsAllow entries appear.
	SkillsMode string `json:"skills_mode"`
	// SkillsAllow is the skill allowlist used when SkillsMode is "custom".
	SkillsAllow []string `json:"skills_allow"`
	// Timezone is the IANA name for wall-clock schedules (default Asia/Jakarta).
	// Jobs may override it per job. Empty means the default.
	Timezone string `json:"timezone"`
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

// DefaultBuildPromptPath returns the default build-prompt.md path.
// The file lives outside the workspace, beside config.json, so workspace
// restrictions never block prompt edits.
func DefaultBuildPromptPath() string { return filepath.Join(DefaultDir(), "build-prompt.md") }

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
	switch workspace.NormalizeSkillsMode(c.SkillsMode) {
	case workspace.SkillsModeDefault, workspace.SkillsModeOff, workspace.SkillsModeCustom:
		c.SkillsMode = workspace.NormalizeSkillsMode(c.SkillsMode)
	default:
		return nil, fmt.Errorf("config %s: skills_mode must be default, off, or custom", path)
	}
	if strings.TrimSpace(c.Timezone) == "" {
		c.Timezone = DefaultTimezone
	} else if _, err := time.LoadLocation(strings.TrimSpace(c.Timezone)); err != nil {
		return nil, fmt.Errorf("config %s: unknown timezone %q (use IANA like Asia/Jakarta)", path, c.Timezone)
	} else {
		c.Timezone = strings.TrimSpace(c.Timezone)
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
	if err := os.MkdirAll(filepath.Join(DefaultDir(), "skillstate"), 0o755); err != nil {
		return nil, fmt.Errorf("create skillstate dir: %w", err)
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

// SkillsPolicy reports the skill injection policy for the system prompt
// (picoclaw turn_profile.skills-like).
func (c *Config) SkillsPolicy() workspace.SkillsPolicy {
	if c == nil {
		return workspace.SkillsPolicy{}
	}
	return workspace.SkillsPolicy{Mode: c.SkillsMode, Allow: c.SkillsAllow}
}

// MemoryPath is <workspace>/memory/MEMORY.md — single memory file, local.
func (c *Config) MemoryPath() string { return workspace.MemoryPath(c.Workspace) }

// MemoryDir is <workspace>/memory (MEMORY.md plus context/ summaries).
func (c *Config) MemoryDir() string { return workspace.MemoryDir(c.Workspace) }

// ContextDir is <workspace>/memory/context (system-managed summaries).
func (c *Config) ContextDir() string { return workspace.ContextDir(c.Workspace) }

// SkillsDir is <workspace>/skills (one SKILL.md per installed skill).
func (c *Config) SkillsDir() string { return workspace.SkillsDir(c.Workspace) }

// BuildPromptPath is ~/.puru/build-prompt.md (editable prompt structure).
func (c *Config) BuildPromptPath() string { return DefaultBuildPromptPath() }

// HistoryDir is ~/.puru/history (per-chat JSON files).
func (c *Config) HistoryDir() string { return filepath.Join(DefaultDir(), "history") }

// SkillStateDir is ~/.puru/skillstate (per-chat active skill names).
func (c *Config) SkillStateDir() string { return filepath.Join(DefaultDir(), "skillstate") }

// EffectiveTimezone reports the IANA timezone for wall-clock schedules.
// Empty or unknown falls back to DefaultTimezone.
func (c *Config) EffectiveTimezone() string {
	if c == nil {
		return DefaultTimezone
	}
	tz := strings.TrimSpace(c.Timezone)
	if tz == "" {
		return DefaultTimezone
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return DefaultTimezone
	}
	return tz
}

// ScheduleDir is <workspace>/schedule (jobs.json for scheduled tasks).
func (c *Config) ScheduleDir() string { return filepath.Join(c.Workspace, "schedule") }
