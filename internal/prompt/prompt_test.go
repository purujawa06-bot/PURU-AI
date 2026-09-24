package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/purujawa06-bot/PURU-AI/internal/workspace"
)

func TestGetRendersMemory(t *testing.T) {
	ws := t.TempDir()
	if err := workspace.Ensure(ws); err != nil {
		t.Fatal(err)
	}
	out, err := Get("memory-x", "summary-y", ws, workspace.SkillsPolicy{})
	if err != nil {
		t.Fatalf("template error: %v", err)
	}
	if !strings.Contains(out, "memory-x") {
		t.Fatalf("memory not injected")
	}
	if !strings.Contains(out, "summary-y") {
		t.Fatalf("summary not injected")
	}
	if !strings.Contains(out, ws) {
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
		"## Skills",
		"The following skills extend your capabilities.",
		"read its SKILL.md file using the read_file tool",
		"<skills>",
		"<source>workspace</source>",
		"find-skills",
		"skill-creator",
		"memory/MEMORY.md",
		"memory/context/",
	} {
		if !strings.Contains(out, section) {
			t.Fatalf("section %q missing in prompt", section)
		}
	}
	// Skill install tutorial lives in the find-skills SKILL.md now,
	// never inline in the system prompt (picoclaw 1:1).
	for _, inline := range []string{
		"find-skills?query=",
		"install-skills?source=",
		"Manage skills",
		"## Active Skills",
	} {
		if strings.Contains(out, inline) {
			t.Fatalf("inline %q must not be in prompt", inline)
		}
	}
	if strings.Contains(out, "e2b") {
		t.Fatalf("old tool references must be gone: %s", out)
	}
}

func TestGetRendersActiveSkills(t *testing.T) {
	ws := t.TempDir()
	if err := workspace.Ensure(ws); err != nil {
		t.Fatal(err)
	}
	agents := "---\nskills: [find-skills]\n---\n\n# Agent\n"
	if err := os.WriteFile(filepath.Join(ws, workspace.FileAgents), []byte(agents), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Get("", "", ws, workspace.SkillsPolicy{})
	if err != nil {
		t.Fatalf("template error: %v", err)
	}
	for _, section := range []string{
		"## Active Skills",
		"active for this request",
		"### Skill: find-skills",
		"PuruBoy",
	} {
		if !strings.Contains(out, section) {
			t.Fatalf("active section %q missing in prompt", section)
		}
	}
	if strings.Contains(out, "skills: [find-skills]") {
		t.Fatalf("frontmatter must not leak into prompt")
	}
}

func TestGetSkillsOffSuppressesAll(t *testing.T) {
	ws := t.TempDir()
	if err := workspace.Ensure(ws); err != nil {
		t.Fatal(err)
	}
	agents := "---\nskills: [find-skills]\n---\n\n# Agent\n"
	if err := os.WriteFile(filepath.Join(ws, workspace.FileAgents), []byte(agents), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Get("", "", ws, workspace.SkillsPolicy{Mode: workspace.SkillsModeOff})
	if err != nil {
		t.Fatalf("template error: %v", err)
	}
	for _, banned := range []string{"## Skills", "## Active Skills", "<skills>", "find-skills"} {
		if strings.Contains(out, banned) {
			t.Fatalf("off policy must drop %q", banned)
		}
	}
}

func TestGetSkillsCustomAllowlist(t *testing.T) {
	ws := t.TempDir()
	if err := workspace.Ensure(ws); err != nil {
		t.Fatal(err)
	}
	agents := "---\nskills: [find-skills, skill-creator]\n---\n\n# Agent\n"
	if err := os.WriteFile(filepath.Join(ws, workspace.FileAgents), []byte(agents), 0o644); err != nil {
		t.Fatal(err)
	}
	policy := workspace.SkillsPolicy{Mode: workspace.SkillsModeCustom, Allow: []string{"find-skills"}}
	out, err := Get("", "", ws, policy)
	if err != nil {
		t.Fatalf("template error: %v", err)
	}
	if !strings.Contains(out, "### Skill: find-skills") {
		t.Fatalf("allowlisted active skill must stay")
	}
	if strings.Contains(out, "skill-creator") {
		t.Fatalf("blocked skill must not appear")
	}
}
