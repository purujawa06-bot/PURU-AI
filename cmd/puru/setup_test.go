package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/purujawa06-bot/PURU-AI/internal/config"
)

// isolateSetupEnv points the config/workspace dirs at a temp home and clears the
// env a developer shell may carry, so runSetup tests are hermetic and never ask
// questions.
func isolateSetupEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	t.Setenv("CONFIG", "")
	t.Setenv("PURU_CONFIG", "")
	t.Setenv("PURU_NON_INTERACTIVE", "1")
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("PURU_BASE_URL", "")
	t.Setenv("PURU_API_KEY", "")
	t.Setenv("PURU_MODEL", "")
	t.Setenv("PURU_WORKSPACE", "")
	return home
}

// writeJSON writes a config file and returns its path.
func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Existing config values must survive a re-run: the wizard offers them as
// defaults, so empty answers (and the minimal wizard itself) keep the setup.
func TestRunSetupKeepsExistingConfigValues(t *testing.T) {
	home := isolateSetupEnv(t)
	t.Setenv("TELEGRAM_BOT_TOKEN", "333:env-token") // env still wins over the file

	path := filepath.Join(t.TempDir(), "config.json")
	writeJSON(t, path, map[string]any{
		"telegram_bot_token":     "111:old-token",
		"telegram_allowed_users": []int64{7},
		"model": map[string]any{
			"base_url":    "http://old.example/v1",
			"api_key":     "old-key",
			"model":       "old-model",
			"temperature": 0.7,
		},
		"workspace":           filepath.Join(home, "old-workspace"),
		"restrict_workspace":  false,
		"max_iterations":      250,
		"history_token_limit": 12345,
		"host":                "127.0.0.1",
		"port":                9999,
		"tools_preview":       false,
		"loop_delay_seconds":  9,
		"exec_memory_mb":      128,
		"skills_mode":         "custom",
		"skills_allow":        []string{"find-skills"},
		"timezone":            "Asia/Makassar",
		"web_search": map[string]any{
			"aistudio": map[string]any{"active": true, "model": "gemini-old", "api_key": "ai-key"},
			"exa":      map[string]any{"active": true, "api_key": "exa-key"},
		},
	})

	if err := runSetup([]string{"--force", "--config", path}); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if got.TelegramBotToken != "333:env-token" {
		t.Fatalf("env token must win, got %q", got.TelegramBotToken)
	}
	if len(got.TelegramAllowedUsers) != 1 || got.TelegramAllowedUsers[0] != 7 {
		t.Fatalf("allowed users not kept: %v", got.TelegramAllowedUsers)
	}
	if got.Model.BaseURL != "http://old.example/v1" || got.Model.Model != "old-model" || got.Model.APIKey != "old-key" {
		t.Fatalf("model not kept: %+v", got.Model)
	}
	if got.Model.Temperature != 0.7 {
		t.Fatalf("temperature not kept: %v", got.Model.Temperature)
	}
	if got.Workspace != filepath.Join(home, "old-workspace") {
		t.Fatalf("workspace not kept: %q", got.Workspace)
	}
	if got.RestrictWorkspace {
		t.Fatal("restrict_workspace=false must be kept")
	}
	if got.MaxIterations != 250 || got.HistoryTokenLimit != 12345 {
		t.Fatalf("runtime limits not kept: %d/%d", got.MaxIterations, got.HistoryTokenLimit)
	}
	if got.Host != "127.0.0.1" || got.Port != 9999 {
		t.Fatalf("health server not kept: %s:%d", got.Host, got.Port)
	}
	if got.ShowToolsPreview() {
		t.Fatal("tools_preview=false must be kept")
	}
	if got.LoopDelaySeconds != 9 || got.ExecMemoryMB != 128 {
		t.Fatalf("loop delay/exec memory not kept: %d/%d", got.LoopDelaySeconds, got.ExecMemoryMB)
	}
	if got.SkillsMode != "custom" || len(got.SkillsAllow) != 1 || got.SkillsAllow[0] != "find-skills" {
		t.Fatalf("skills not kept: %q %v", got.SkillsMode, got.SkillsAllow)
	}
	if got.Timezone != "Asia/Makassar" {
		t.Fatalf("timezone not kept: %q", got.Timezone)
	}
	if !got.WebSearch.AIStudio.Ready() || got.WebSearch.AIStudio.Model != "gemini-old" {
		t.Fatalf("aistudio search not kept: %+v", got.WebSearch.AIStudio)
	}
	if !got.WebSearch.Exa.Ready() || got.WebSearch.Exa.APIKey != "exa-key" {
		t.Fatalf("exa search not kept: %+v", got.WebSearch.Exa)
	}
}

// A fresh install (no config, no env) must still land on the built-in defaults.
func TestRunSetupFreshConfigUsesBuiltInDefaults(t *testing.T) {
	home := isolateSetupEnv(t)
	t.Setenv("TELEGRAM_BOT_TOKEN", "222:new-token")

	path := filepath.Join(t.TempDir(), "config.json")
	if err := runSetup([]string{"--force", "--config", path}); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if got.TelegramBotToken != "222:new-token" {
		t.Fatalf("unexpected token: %q", got.TelegramBotToken)
	}
	if got.Model.BaseURL != "https://api.openai.com/v1" || got.Model.Model != "gpt-4o-mini" {
		t.Fatalf("unexpected model defaults: %+v", got.Model)
	}
	if got.Workspace != filepath.Join(home, ".puru", "workspace") {
		t.Fatalf("unexpected workspace: %q", got.Workspace)
	}
	if !got.RestrictWorkspace || !got.ShowToolsPreview() {
		t.Fatalf("expected restrict_workspace and tools_preview on: %v/%v", got.RestrictWorkspace, got.ShowToolsPreview())
	}
	if got.MaxIterations != config.DefaultMaxIterations || got.Timezone != config.DefaultTimezone {
		t.Fatalf("unexpected defaults: %d/%s", got.MaxIterations, got.Timezone)
	}
	if len(got.TelegramAllowedUsers) != 0 || len(got.SkillsAllow) != 0 {
		t.Fatalf("expected empty lists, got %v/%v", got.TelegramAllowedUsers, got.SkillsAllow)
	}
}

func TestLoadPreviousConfigHandlesMissingAndBrokenFiles(t *testing.T) {
	dir := t.TempDir()

	missing, ok := loadPreviousConfig(filepath.Join(dir, "nope.json"))
	if ok || missing == nil || missing.restrictWorkspaceDefault() != true {
		t.Fatalf("missing file must fall back to zero values with jail on, got ok=%v", ok)
	}

	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if prev, ok := loadPreviousConfig(broken); ok || prev == nil {
		t.Fatalf("broken file must be ignored, got ok=%v", ok)
	}
}
