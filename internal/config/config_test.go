package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCfg(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDefaults(t *testing.T) {
	p := writeCfg(t, `{"telegram_bot_token":"x","model":{"base_url":"http://m/v1","model":"puru"}}`)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxIterations != DefaultMaxIterations {
		t.Errorf("MaxIterations = %d, want %d", c.MaxIterations, DefaultMaxIterations)
	}
	if c.HistoryTokenLimit != DefaultHistoryTokLimit {
		t.Errorf("HistoryTokenLimit = %d, want %d", c.HistoryTokenLimit, DefaultHistoryTokLimit)
	}
	if !c.RestrictWorkspace && c.Workspace == "" {
		t.Errorf("workspace default tidak diterapkan")
	}
	if c.MemoryPath() == "" || c.HistoryDir() == "" {
		t.Errorf("MemoryPath/HistoryDir kosong")
	}
}

func TestLoadRejectsEmptyToken(t *testing.T) {
	p := writeCfg(t, `{"model":{"base_url":"http://m/v1","model":"puru"}}`)
	if _, err := Load(p); err == nil {
		t.Errorf("expected error untuk token kosong")
	}
}

func TestResolvePathPrecedence(t *testing.T) {
	t.Setenv("PURU_CONFIG", "/tmp/env.json")
	if got := ResolvePath("/tmp/flag.json"); got != "/tmp/flag.json" {
		t.Errorf("flag harus menang, got %s", got)
	}
	if got := ResolvePath(""); got != "/tmp/env.json" {
		t.Errorf("env harus dipakai, got %s", got)
	}
}
