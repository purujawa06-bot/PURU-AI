package prompt

import (
	"strings"
	"testing"
)

func TestGetRendersMemory(t *testing.T) {
	out, err := Get("memory-x", "summary-y", "/ws")
	if err != nil {
		t.Fatalf("template error: %v", err)
	}
	if !strings.Contains(out, "memory-x") {
		t.Fatalf("memory not injected")
	}
	if !strings.Contains(out, "summary-y") {
		t.Fatalf("summary not injected")
	}
	if !strings.Contains(out, "/ws") {
		t.Fatalf("workspace not injected")
	}
	for _, tool := range []string{"read_file", "write_file", "list_dir", "edit_file_replace_string", "edit_file_replace_line", "edit_file_apply_patch", "append_file", "exec", "telegram_sendfile", "telegram_getuser"} {
		if !strings.Contains(out, tool) {
			t.Fatalf("tool %s missing in prompt", tool)
		}
	}
	// puruClaw identity (picoclaw personality, renamed): no leftover
	// picoclaw/Pico references allowed.
	for _, name := range []string{"puruClaw", "PuruClaw", "Puru"} {
		if !strings.Contains(out, name) {
			t.Fatalf("identity %q missing in prompt", name)
		}
	}
	for _, stale := range []string{"picoclaw", "PicoClaw", "Pico", "PURU-AI"} {
		if strings.Contains(out, stale) {
			t.Fatalf("stale reference %q must be gone", stale)
		}
	}
	for _, section := range []string{
		"ALWAYS use tools",
		"Be helpful and accurate",
		"Context summaries",
		"Working Principles",
		"Personality",
		"Values",
	} {
		if !strings.Contains(out, section) {
			t.Fatalf("section %q missing in prompt", section)
		}
	}
	if strings.Contains(out, "e2b") {
		t.Fatalf("old tool references must be gone: %s", out)
	}
}
