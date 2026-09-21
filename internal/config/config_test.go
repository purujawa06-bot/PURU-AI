package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
	if c.Host != DefaultHealthHost {
		t.Errorf("Host = %q, want %q", c.Host, DefaultHealthHost)
	}
	if c.Port != DefaultHealthPort {
		t.Errorf("Port = %d, want %d", c.Port, DefaultHealthPort)
	}
	if !c.ShowToolsPreview() {
		t.Errorf("ShowToolsPreview default harus true")
	}
	if c.LoopDelaySeconds != DefaultLoopDelaySeconds {
		t.Errorf("LoopDelaySeconds = %d, want %d", c.LoopDelaySeconds, DefaultLoopDelaySeconds)
	}
	if c.ExecMemoryMB != DefaultExecMemoryMB {
		t.Errorf("ExecMemoryMB = %d, want %d", c.ExecMemoryMB, DefaultExecMemoryMB)
	}
}

func TestToolsPreviewExplicit(t *testing.T) {
	p := writeCfg(t, `{"telegram_bot_token":"x","model":{"base_url":"http://m/v1","model":"puru"},"tools_preview":false,"loop_delay_seconds":10}`)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.ShowToolsPreview() {
		t.Errorf("ShowToolsPreview harus false bila di-set false")
	}
	if c.LoopDelay() != 10*time.Second {
		t.Errorf("LoopDelay = %v, want 10s", c.LoopDelay())
	}
	p = writeCfg(t, `{"telegram_bot_token":"x","model":{"base_url":"http://m/v1","model":"puru"},"loop_delay_seconds":999}`)
	c, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.LoopDelaySeconds != MaxLoopDelaySeconds {
		t.Errorf("LoopDelaySeconds harus di-clamp ke %d, got %d", MaxLoopDelaySeconds, c.LoopDelaySeconds)
	}
	p = writeCfg(t, `{"telegram_bot_token":"x","model":{"base_url":"http://m/v1","model":"puru"},"exec_memory_mb":10}`)
	c, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.ExecMemoryMB != MinExecMemoryMB {
		t.Errorf("ExecMemoryMB harus di-clamp ke min %d, got %d", MinExecMemoryMB, c.ExecMemoryMB)
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
