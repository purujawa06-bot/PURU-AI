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
	for _, tool := range []string{"read_file", "write_file", "list_dir", "edit_file", "append_file", "run_shell_command", "telegram_sendfile", "telegram_getuser"} {
		_ = tool
	}
	// Picoclaw 1:1 — tools are declared via native function calls only and
	// must not be embedded as a list in the system prompt.
	for _, banned := range []string{"## Tools", "web_search —", "web_fetch —", "telegram_sendfile —", "telegram_getuser —", "edit_file —"} {
		if strings.Contains(out, banned) {
			t.Fatalf("tool list %q must not be in prompt", banned)
		}
	}
	// puruClaw identity (picoclaw personality, renamed): no leftover
	// picoclaw/Pico references allowed.
	for _, name := range []string{"PuruClaw", "A helpful AI assistant"} {
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
		"Onboarding placeholders",
		"Greet warmly",
		"introduce yourself as PuruClaw",
		"PLACEHOLDER",
		"Working Principles",
		"Personality",
		"Values",
		"## Skills",
		"The following skills extend your capabilities.",
		"They are NOT loaded",
		"call use_skill",
		"<skills>",
		"<source>workspace</source>",
		"find-skills",
		"skill-creator",
		"puruclaw-configure",
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
		"skills.sh",
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

// TestGetSelfHealsMissingFiles ensures every new prompt restores deleted
// bootstrap files from embedded defaults so physical files always exist.
func TestGetSelfHealsMissingFiles(t *testing.T) {
	ws := t.TempDir()
	if err := workspace.Ensure(ws); err != nil {
		t.Fatal(err)
	}
	deleted := []string{
		filepath.Join(ws, workspace.FileAgents),
		filepath.Join(ws, workspace.FileSoul),
		filepath.Join(ws, workspace.FileUser),
		workspace.MemoryPath(ws),
	}
	for _, path := range deleted {
		if err := os.Remove(path); err != nil {
			t.Fatalf("remove %s: %v", path, err)
		}
	}
	if _, err := Get("", "", ws, workspace.SkillsPolicy{}); err != nil {
		t.Fatalf("template error: %v", err)
	}
	for _, path := range deleted {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected %s to be restored: %v", path, err)
		}
		if strings.TrimSpace(string(data)) == "" {
			t.Fatalf("%s must not be empty after restore", path)
		}
	}
}

func TestBuildIncludesRuntimeContext(t *testing.T) {
	ws := t.TempDir()
	if err := workspace.Ensure(ws); err != nil {
		t.Fatal(err)
	}
	out, err := Build(Request{
		Workspace:         ws,
		Channel:           "telegram",
		ChatID:            "123",
		SenderID:          "7",
		SenderDisplayName: "Budi (@budi)",
	})
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	for _, section := range []string{
		"## Current Time",
		"## Runtime",
		"## Current Session",
		"Channel: telegram",
		"Chat ID: 123",
		"Current sender: Budi (@budi) (ID: 7)",
	} {
		if !strings.Contains(out, section) {
			t.Fatalf("runtime section %q missing in prompt", section)
		}
	}
}

func TestBuildOrdersLayersLikePicoclaw(t *testing.T) {
	ws := t.TempDir()
	if err := workspace.Ensure(ws); err != nil {
		t.Fatal(err)
	}
	out, err := Build(Request{Workspace: ws, Memory: "memory-x", Summary: "summary-y"})
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	order := []string{
		"# PuruClaw 🦞",
		"## " + workspace.FileAgents,
		"## Skills",
		"## Memory",
		"## Current Time",
		"CONTEXT_SUMMARY:",
	}
	last := -1
	for _, marker := range order {
		idx := strings.Index(out, marker)
		if idx < 0 {
			t.Fatalf("marker %q missing in prompt", marker)
		}
		if idx < last {
			t.Fatalf("marker %q out of order", marker)
		}
		last = idx
	}
}

func TestBuildSuppressFlags(t *testing.T) {
	ws := t.TempDir()
	if err := workspace.Ensure(ws); err != nil {
		t.Fatal(err)
	}
	out, err := Build(Request{Workspace: ws, SuppressSkillContext: true})
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	for _, banned := range []string{"## Skills", "## Active Skills", "<skills>"} {
		if strings.Contains(out, banned) {
			t.Fatalf("suppressed %q must not appear", banned)
		}
	}
	out, err = Build(Request{Workspace: ws, SuppressToolUseRule: true})
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if strings.Contains(out, "**ALWAYS use tools**") {
		t.Fatalf("tool use rule must be suppressed")
	}
	overlay := PromptPart{
		ID:      "instruction.subturn_profile",
		Layer:   PromptLayerInstruction,
		Slot:    PromptSlotWorkspace,
		Source:  PromptSource{ID: PromptSourceSubTurnProfile, Name: "subturn.profile"},
		Title:   "SubTurn System Instructions",
		Content: "follow the subturn profile",
	}
	out, err = Build(Request{SuppressDefaultSystemPrompt: true, Overlays: []PromptPart{overlay}})
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if !strings.Contains(out, "follow the subturn profile") {
		t.Fatalf("overlay must survive suppressed system prompt")
	}
	if strings.Contains(out, "# PuruClaw 🦞") {
		t.Fatalf("kernel identity must be suppressed")
	}
	out, err = Build(Request{SuppressDefaultSystemPrompt: true, ToolUseFallback: true})
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if !strings.Contains(out, "**ALWAYS use tools**") {
		t.Fatalf("tool use fallback must render when system prompt is suppressed")
	}
}

func TestRegistryValidatesPlacement(t *testing.T) {
	r := NewPromptRegistry()
	valid := PromptPart{
		ID:      "kernel.identity",
		Layer:   PromptLayerKernel,
		Slot:    PromptSlotIdentity,
		Source:  PromptSource{ID: PromptSourceKernel},
		Content: "x",
	}
	if err := r.ValidatePart(valid); err != nil {
		t.Fatalf("valid part rejected: %v", err)
	}
	invalid := PromptPart{
		ID:      "bad",
		Layer:   PromptLayerTurn,
		Slot:    PromptSlotMessage,
		Source:  PromptSource{ID: PromptSourceKernel},
		Content: "x",
	}
	if err := r.ValidatePart(invalid); err == nil {
		t.Fatalf("invalid placement must be rejected")
	}
	compat := PromptPart{
		ID:      "compat",
		Layer:   PromptLayerTurn,
		Slot:    PromptSlotMessage,
		Source:  PromptSource{ID: "custom.source"},
		Content: "x",
	}
	if err := r.ValidatePart(compat); err != nil {
		t.Fatalf("unregistered source must be allowed in compatibility mode: %v", err)
	}
	stack := NewPromptStack(r)
	if err := stack.Add(PromptPart{ID: "empty", Layer: PromptLayerKernel, Slot: PromptSlotIdentity, Source: PromptSource{ID: PromptSourceKernel}}); err != nil {
		t.Fatalf("empty content must be skipped without error: %v", err)
	}
	if len(stack.Parts()) != 0 {
		t.Fatalf("empty content must not be stored")
	}
	if err := stack.Add(valid); err != nil {
		t.Fatalf("stack add failed: %v", err)
	}
	stack.Seal()
	if err := stack.Add(valid); err == nil {
		t.Fatalf("sealed stack must reject writes")
	}
}

func TestBuildHidesOnboardingWhenPlaceholdersFilled(t *testing.T) {
	ws := t.TempDir()
	if err := workspace.Ensure(ws); err != nil {
		t.Fatal(err)
	}
	filled := "# User\n\n- Name: Budi\n"
	if err := os.WriteFile(filepath.Join(ws, workspace.FileUser), []byte(filled), 0o644); err != nil {
		t.Fatal(err)
	}
	memory := "# Long-term Memory\n\n- User is Budi.\n"
	if err := os.WriteFile(workspace.MemoryPath(ws), []byte(memory), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Build(Request{Workspace: ws, Memory: memory})
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if strings.Contains(out, "Onboarding placeholders") {
		t.Fatalf("onboarding rule must be hidden when all placeholders are filled")
	}
}

func TestBuildShowsOnboardingWhilePlaceholderRemains(t *testing.T) {
	ws := t.TempDir()
	if err := workspace.Ensure(ws); err != nil {
		t.Fatal(err)
	}
	filledMemory := "# Long-term Memory\n\n- User is Budi.\n"
	if err := os.WriteFile(workspace.MemoryPath(ws), []byte(filledMemory), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Build(Request{Workspace: ws, Memory: filledMemory})
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if !strings.Contains(out, "Onboarding placeholders") {
		t.Fatalf("onboarding rule must stay while USER.md still has placeholders")
	}
}
