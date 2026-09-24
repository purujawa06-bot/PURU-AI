package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureSeedsBootstrapFiles(t *testing.T) {
	ws := t.TempDir()
	if err := Ensure(ws); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		FileAgents,
		FileSoul,
		FileUser,
		filepath.Join(DirMemory, FileMemory),
	} {
		data, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("expected %s to be seeded: %v", name, err)
		}
		if strings.TrimSpace(string(data)) == "" {
			t.Fatalf("%s must not be empty", name)
		}
	}
	for _, dir := range []string{MemoryDir(ws), ContextDir(ws), SkillsDir(ws)} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("%s must exist: %v", dir, err)
		}
	}
}

func TestEnsureNeverOverwrites(t *testing.T) {
	ws := t.TempDir()
	custom := "# Custom agent\n"
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, FileAgents), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(ws); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(ws, FileAgents))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != custom {
		t.Fatalf("existing AGENTS.md must be kept, got %q", got)
	}
}

func TestAgentsPathPrefersPlural(t *testing.T) {
	ws := t.TempDir()
	if got := AgentsPath(ws); filepath.Base(got) != FileAgents {
		t.Fatalf("missing files must default to AGENTS.md, got %q", got)
	}
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, FileAgent), []byte("# legacy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := AgentsPath(ws); filepath.Base(got) != FileAgent {
		t.Fatalf("AGENT.md alias must be used when present, got %q", got)
	}
	if err := os.WriteFile(filepath.Join(ws, FileAgents), []byte("# primary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := AgentsPath(ws); filepath.Base(got) != FileAgents {
		t.Fatalf("AGENTS.md must win over AGENT.md, got %q", got)
	}
}

func TestLoadBootstrap(t *testing.T) {
	ws := t.TempDir()
	if err := Ensure(ws); err != nil {
		t.Fatal(err)
	}
	def := Load(ws)
	if !strings.Contains(def.AgentsBody, "Puru") {
		t.Fatalf("AgentsBody must load, got %q", def.AgentsBody)
	}
	if !strings.Contains(def.Soul, "PuruClaw") {
		t.Fatalf("Soul must load, got %q", def.Soul)
	}
	boot := def.Bootstrap()
	for _, label := range []string{"## " + FileAgents, "## " + FileSoul, "## " + FileUser} {
		if !strings.Contains(boot, label) {
			t.Fatalf("bootstrap must contain %q", label)
		}
	}
}

func TestMemoryAndContextInsideMemory(t *testing.T) {
	ws := t.TempDir()
	if err := Ensure(ws); err != nil {
		t.Fatal(err)
	}
	memory := MemoryPath(ws)
	context := ContextDir(ws)
	if filepath.Dir(memory) != MemoryDir(ws) {
		t.Fatalf("MEMORY.md must live in memory/: %q", memory)
	}
	if filepath.Dir(context) != MemoryDir(ws) {
		t.Fatalf("context/ must live in memory/: %q", context)
	}
}

func TestSkillsCatalog(t *testing.T) {
	ws := t.TempDir()
	if err := Ensure(ws); err != nil {
		t.Fatal(err)
	}
	if got := BuildSkillsSummary(ws); got != "" {
		t.Fatalf("empty skills must render empty summary, got %q", got)
	}
	skillDir := filepath.Join(SkillsDir(ws), "web-design-guidelines")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: web-design-guidelines\ndescription: Review UI code for guidelines compliance.\n---\n\n# Skill\n"
	if err := os.WriteFile(filepath.Join(skillDir, FileSkill), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	installed := ListSkills(ws)
	if len(installed) != 1 || installed[0].Name != "web-design-guidelines" {
		t.Fatalf("ListSkills = %+v", installed)
	}
	summary := BuildSkillsSummary(ws)
	if !strings.Contains(summary, "web-design-guidelines") || !strings.Contains(summary, "Review UI code") {
		t.Fatalf("summary must list skill name and description, got %q", summary)
	}
}
