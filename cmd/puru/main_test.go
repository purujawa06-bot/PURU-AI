package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNormalizeYes(t *testing.T) {
	if !normalizeYes("y", false) || !normalizeYes("YES", false) || !normalizeYes("ya", false) {
		t.Fatal("expected yes variants true")
	}
	if normalizeYes("n", true) || normalizeYes("no", true) {
		t.Fatal("expected no variants false")
	}
	if !normalizeYes("", true) || normalizeYes("", false) {
		t.Fatal("expected default on empty")
	}
	if normalizeYes("ngawur", true) != true || normalizeYes("ngawur", false) != false {
		t.Fatal("expected fallback to default")
	}
}

func TestParseGatewayDefaults(t *testing.T) {
	o, err := parseGatewayArgs([]string{})
	if err != nil {
		t.Fatal(err)
	}
	if o.withHealth {
		t.Fatal("gateway default harus tanpa health server")
	}
	o2, err := parseGatewayArgs([]string{"--health", "--port", "9090"})
	if err != nil {
		t.Fatal(err)
	}
	if !o2.withHealth || o2.port != 9090 {
		t.Fatalf("unexpected gateway opts: %+v", o2)
	}
}

func TestParseSetupArgs(t *testing.T) {
	o, err := parseSetupArgs([]string{"--force"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.force {
		t.Fatal("expected force true")
	}
	if _, err := parseSetupArgs([]string{"extra"}); err == nil {
		t.Fatal("expected error on extra arg")
	}
}

func TestWriteConfigJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".puru", "config.json")
	cfg := map[string]any{
		"telegram_bot_token": "123:abc",
		"model":              map[string]any{"base_url": "http://x/v1", "model": "puru"},
	}
	if err := writeConfigJSON(path, cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["telegram_bot_token"] != "123:abc" {
		t.Fatalf("unexpected token: %v", got)
	}
	fi, _ := os.Stat(path)
	// Windows tidak menerapkan mode unix 0600 (selalu 0666) — skip di sana.
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600, got %o", fi.Mode().Perm())
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if firstNonEmpty("", "  ", "ok") != "ok" {
		t.Fatal("expected ok")
	}
	if firstNonEmpty("", "") != "" {
		t.Fatal("expected empty")
	}
}
