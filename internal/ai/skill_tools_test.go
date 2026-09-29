package ai

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/purujawa06-bot/PURU-AI/internal/config"
	"github.com/purujawa06-bot/PURU-AI/internal/workspace"
)

func skillTestAgent(ws string) *Agent {
	return &Agent{Config: &config.Config{Workspace: ws, RestrictWorkspace: true}}
}

func seedFrontmatter(t *testing.T, ws, agents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(ws, workspace.FileAgents), []byte(agents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestActiveSkillsForNilSafe(t *testing.T) {
	if got := ActiveSkillsFor(nil, nil); len(got) != 0 {
		t.Fatalf("nil agent must yield empty, got %v", got)
	}
	a := &Agent{Config: nil}
	if got := ActiveSkillsFor(a, &ProcessOptions{ChatID: 1}); len(got) != 0 {
		t.Fatalf("nil config must yield empty, got %v", got)
	}
	if got := ActiveSkillsFor(skillTestAgent(t.TempDir()), nil); len(got) != 0 {
		t.Fatalf("nil opts must yield empty, got %v", got)
	}
}

func TestIsActiveSkillPathFrontmatter(t *testing.T) {
	ws := t.TempDir()
	if err := workspace.Ensure(ws); err != nil {
		t.Fatal(err)
	}
	seedFrontmatter(t, ws, "---\nskills: [find-skills]\n---\n\n# Agent\n")
	a := skillTestAgent(ws)
	opts := &ProcessOptions{ChatID: 42}
	if name, ok := isActiveSkillPath(a, opts, "skills/find-skills/SKILL.md"); !ok || name == "" {
		t.Fatalf("active skill path must be guarded, got %q,%v", name, ok)
	}
	if _, ok := isActiveSkillPath(a, opts, "skills/skill-creator/SKILL.md"); ok {
		t.Fatalf("inactive skill must not be guarded")
	}
	if _, ok := isActiveSkillPath(a, opts, "AGENTS.md"); ok {
		t.Fatalf("non-skill path must not be guarded")
	}
}

func TestRejectActiveSkillExec(t *testing.T) {
	ws := t.TempDir()
	if err := workspace.Ensure(ws); err != nil {
		t.Fatal(err)
	}
	seedFrontmatter(t, ws, "---\nskills: [find-skills]\n---\n\n# Agent\n")
	a := skillTestAgent(ws)
	opts := &ProcessOptions{ChatID: 7}
	if _, ok := rejectActiveSkillExec(a, opts, "rm -rf skills/find-skills"); !ok {
		t.Fatalf("destructive exec on active skill must be rejected")
	}
	if _, ok := rejectActiveSkillExec(a, opts, "ls skills/find-skills"); ok {
		t.Fatalf("read-only exec must stay allowed")
	}
	if _, ok := rejectActiveSkillExec(a, opts, "echo hi"); ok {
		t.Fatalf("unrelated command must stay allowed")
	}
}

func TestSkillToolsRegistered(t *testing.T) {
	ws := t.TempDir()
	tools := BuildTools(skillTestAgent(ws), nil)
	for _, name := range []string{"use_skill", "stop_skill"} {
		tool := tools[name]
		if tool == nil {
			t.Fatalf("tool %s missing", name)
		}
		params, _ := tool.Parameters["properties"].(map[string]any)
		if params["name"] == nil {
			t.Fatalf("%s must declare name param", name)
		}
	}
}

func TestUseSkillRejectsUnknown(t *testing.T) {
	ws := t.TempDir()
	if err := workspace.Ensure(ws); err != nil {
		t.Fatal(err)
	}
	a := skillTestAgent(ws)
	tools := BuildTools(a, &ProcessOptions{ChatID: 42})
	out, _ := tools["use_skill"].Run(context.Background(), map[string]any{"name": "no-such-skill"})
	result, _ := out.(map[string]any)
	if result["success"] != false {
		t.Fatalf("unknown skill must fail, got %v", out)
	}
}

func TestStopSkillRequiresChatScope(t *testing.T) {
	ws := t.TempDir()
	a := skillTestAgent(ws)
	tools := BuildTools(a, nil)
	out, _ := tools["stop_skill"].Run(context.Background(), map[string]any{"name": "find-skills"})
	result, _ := out.(map[string]any)
	if result["success"] != false {
		t.Fatalf("stop without chat scope must fail, got %v", out)
	}
}

func TestActiveSkillDirectEditAllowed(t *testing.T) {
	ws := t.TempDir()
	if err := workspace.Ensure(ws); err != nil {
		t.Fatal(err)
	}
	seedFrontmatter(t, ws, "---\nskills: [find-skills]\n---\n\n# Agent\n")
	a := skillTestAgent(ws)
	tools := BuildTools(a, &ProcessOptions{ChatID: 42})
	ctx := context.Background()
	if out, _ := tools["write_file"].Run(ctx, map[string]any{"path": "skills/find-skills/notes.md", "content": "hi"}); hasErrPicoclaw(out) {
		t.Fatalf("write to active skill must be allowed, got %v", out)
	}
	if out, _ := tools["read_file"].Run(ctx, map[string]any{"path": "skills/find-skills/SKILL.md"}); hasErrPicoclaw(out) {
		t.Fatalf("read of active SKILL.md must be allowed, got %v", out)
	}
}

func TestUseSkillEmptyBody(t *testing.T) {
	ws := t.TempDir()
	skillDir := filepath.Join(ws, "skills", "empty-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: empty-skill\ndescription: Empty body skill.\nversion: 1.0.0\n---\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	a := skillTestAgent(ws)
	tools := BuildTools(a, &ProcessOptions{ChatID: 42})
	out, _ := tools["use_skill"].Run(context.Background(), map[string]any{"name": "empty-skill"})
	if hasErrPicoclaw(out) {
		t.Fatalf("frontmatter-only skill must activate, got %v", out)
	}
}
