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
	for _, skill := range []string{"find-skills", "skill-creator"} {
		data, err := os.ReadFile(SkillFile(ws, skill))
		if err != nil {
			t.Fatalf("builtin skill %s must be seeded: %v", skill, err)
		}
		if strings.TrimSpace(string(data)) == "" {
			t.Fatalf("builtin skill %s must not be empty", skill)
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
	customSkill := "---\nname: find-skills\ndescription: Custom.\n---\n\n# Custom\n"
	skillDir := filepath.Join(SkillsDir(ws), "find-skills")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, FileSkill), []byte(customSkill), 0o644); err != nil {
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
	gotSkill, err := os.ReadFile(filepath.Join(skillDir, FileSkill))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotSkill) != customSkill {
		t.Fatalf("existing skill must be kept, got %q", gotSkill)
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
	summary := BuildSkillsSummary(ws)
	for _, want := range []string{"<skills>", "<source>workspace</source>", "find-skills", "skill-creator"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("builtin catalog must contain %q, got %q", want, summary)
		}
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
	names := map[string]bool{}
	for _, skill := range installed {
		names[skill.Name] = true
		if skill.Source != "workspace" {
			t.Fatalf("skill source must be workspace, got %q", skill.Source)
		}
	}
	for _, want := range []string{"find-skills", "skill-creator", "web-design-guidelines"} {
		if !names[want] {
			t.Fatalf("ListSkills missing %q: %+v", want, installed)
		}
	}
	summary = BuildSkillsSummary(ws)
	if !strings.Contains(summary, "web-design-guidelines") || !strings.Contains(summary, "Review UI code") {
		t.Fatalf("summary must list skill name and description, got %q", summary)
	}
}

func TestLoadSkillsForContext(t *testing.T) {
	ws := t.TempDir()
	if err := Ensure(ws); err != nil {
		t.Fatal(err)
	}
	body, ok := LoadSkill(ws, "FIND-SKILLS")
	if !ok {
		t.Fatal("LoadSkill must resolve names case-insensitively")
	}
	if strings.Contains(body, "name: find-skills") {
		t.Fatal("LoadSkill must strip frontmatter")
	}
	if !strings.Contains(body, "PuruBoy") {
		t.Fatalf("find-skills body must load, got %q", body[:120])
	}
	if _, ok := LoadSkill(ws, "no-such-skill"); ok {
		t.Fatal("unknown skill must return false")
	}
	ctx := LoadSkillsForContext(ws, []string{"find-skills", "no-such-skill"})
	if !strings.Contains(ctx, "### Skill: find-skills") {
		t.Fatalf("context must render skill section, got %q", ctx)
	}
	if strings.Contains(ctx, "no-such-skill") {
		t.Fatalf("unknown skills must be skipped, got %q", ctx)
	}
	if got := LoadSkillsForContext(ws, nil); got != "" {
		t.Fatalf("empty names must render empty, got %q", got)
	}
}

func TestAgentFrontmatterSkills(t *testing.T) {
	ws := t.TempDir()
	if err := Ensure(ws); err != nil {
		t.Fatal(err)
	}
	agents := "---\nskills: [find-skills]\n---\n\n# Agent\n"
	if err := os.WriteFile(filepath.Join(ws, FileAgents), []byte(agents), 0o644); err != nil {
		t.Fatal(err)
	}
	def := Load(ws)
	if len(def.FrontmatterSkills) != 1 || def.FrontmatterSkills[0] != "find-skills" {
		t.Fatalf("FrontmatterSkills = %v", def.FrontmatterSkills)
	}
	if strings.Contains(def.AgentsBody, "skills:") {
		t.Fatalf("frontmatter must be stripped from body, got %q", def.AgentsBody)
	}

	agents = "---\nskills:\n  - skill-creator\n  - find-skills\n---\n\n# Agent\n"
	if err := os.WriteFile(filepath.Join(ws, FileAgents), []byte(agents), 0o644); err != nil {
		t.Fatal(err)
	}
	def = Load(ws)
	if len(def.FrontmatterSkills) != 2 {
		t.Fatalf("block-style FrontmatterSkills = %v", def.FrontmatterSkills)
	}
}
