package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/purujawa06-bot/PURU-AI/internal/workspace"
)

func writeTemplateCopy(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), BuildPromptFilename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEnsureBuildPromptSeedsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), BuildPromptFilename)
	if err := EnsureBuildPrompt(path); err != nil {
		t.Fatalf("ensure failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read seeded file: %v", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		t.Fatalf("seeded file must not be empty")
	}
	if !strings.Contains(string(data), `{{define "identity"}}`) {
		t.Fatalf("seeded file must contain identity section")
	}
	// Existing edits are never overwritten.
	custom := "custom content"
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureBuildPrompt(path); err != nil {
		t.Fatalf("re-ensure failed: %v", err)
	}
	kept, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != custom {
		t.Fatalf("existing file must be preserved")
	}
}

func TestBuildUsesCustomTemplate(t *testing.T) {
	ws := t.TempDir()
	if err := workspace.Ensure(ws); err != nil {
		t.Fatal(err)
	}
	custom := strings.ReplaceAll(defaultBuildPrompt, "You are puruClaw, a helpful AI assistant.", "You are CustomBot, edited via build-prompt.md.")
	path := writeTemplateCopy(t, custom)
	out, err := Build(Request{Workspace: ws, TemplatePath: path})
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if !strings.Contains(out, "You are CustomBot") {
		t.Fatalf("custom template text must appear in prompt")
	}
	if strings.Contains(out, "You are puruClaw, a helpful AI assistant.") {
		t.Fatalf("default identity must be replaced by custom template")
	}
}

func TestBuildFallsBackOnBrokenTemplate(t *testing.T) {
	ws := t.TempDir()
	if err := workspace.Ensure(ws); err != nil {
		t.Fatal(err)
	}
	path := writeTemplateCopy(t, `{{define "identity"}} {{ .Missing`)
	out, err := Build(Request{Workspace: ws, Memory: "memory-x", TemplatePath: path})
	if err != nil {
		t.Fatalf("broken template must not fail build: %v", err)
	}
	if !strings.Contains(out, "# puruClaw") {
		t.Fatalf("broken template must fall back to embedded identity")
	}
	if !strings.Contains(out, "memory-x") {
		t.Fatalf("memory must survive template fallback")
	}
}

func TestBuildSeedsExplicitTemplatePath(t *testing.T) {
	ws := t.TempDir()
	if err := workspace.Ensure(ws); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), BuildPromptFilename)
	if _, err := Build(Request{Workspace: ws, TemplatePath: path}); err != nil {
		t.Fatalf("build error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("explicit template path must be seeded: %v", err)
	}
	if !strings.Contains(string(data), `{{define "identity"}}`) {
		t.Fatalf("seeded template must contain sections")
	}
}
