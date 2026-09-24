// Package workspace manages the PURU-AI workspace layout.
//
// Layout mirrors picoclaw workspace bootstrap (pkg/agent/definition.go +
// pkg/agent/context.go LoadBootstrapFiles), adapted to PURU conventions:
//
//	<workspace>/
//	  AGENTS.md  # agent identity, role, mission (AGENT.md accepted as legacy alias)
//	  SOUL.md    # personality and values
//	  USER.md    # user profile and preferences (optional)
//	  memory/    # long-term memory and summaries (picoclaw-like)
//	    MEMORY.md  # lasting user facts, written by the agent itself
//	    context/   # PURU-specific conversation summaries, system-managed
//	  skills/    # installed skills, one SKILL.md per skill (picoclaw-like)
//
// Bootstrap files (AGENTS.md, SOUL.md, USER.md) are injected into the system
// prompt on every request. MEMORY.md holds lasting user facts written by the
// agent itself. memory/context/ holds model-generated summaries and must never
// be written by the agent directly.
package workspace

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// FileAgents is the primary agent definition file (plural, per PURU convention).
	FileAgents = "AGENTS.md"
	// FileAgent is the legacy singular alias accepted for picoclaw compatibility.
	FileAgent = "AGENT.md"
	// FileSoul holds personality and values, same name as picoclaw workspace/SOUL.md.
	FileSoul = "SOUL.md"
	// FileUser holds workspace user profile, same name as picoclaw workspace/USER.md.
	FileUser = "USER.md"
	// FileMemory is the long-term memory file kept inside memory/
	// (picoclaw-like: memory/MEMORY.md).
	FileMemory = "MEMORY.md"
	// DirMemory holds long-term memory plus conversation summaries.
	DirMemory = "memory"
	// DirContext holds system-managed conversation summaries inside memory/.
	DirContext = "context"
	// DirSkills holds installed skills, one SKILL.md per skill.
	DirSkills = "skills"
	// FileSkill is the skill definition file name (picoclaw-like).
	FileSkill = "SKILL.md"
	// FindSkillsAPI searches installable skills via the PuruBoy agent-tools API.
	FindSkillsAPI = "https://puruboy-api.vercel.app/api/agent-tools/find-skills"
	// InstallSkillsAPI fetches one SKILL.md via the PuruBoy agent-tools API.
	InstallSkillsAPI = "https://puruboy-api.vercel.app/api/agent-tools/install-skills"
)

// DefaultAgentsMD is seeded when neither AGENTS.md nor AGENT.md exists.
// It mirrors picoclaw workspace/AGENT.md (role, mission, capabilities,
// working principles, goals), renamed to Puru.
const DefaultAgentsMD = `# Puru — Default Agent

You are Puru, the default assistant for this workspace.
Your name is PuruClaw.

## Role

You are an ultra-lightweight personal AI assistant written in Go, designed to
be practical, accurate, and efficient.

## Mission

- Help with general requests, questions, and problem solving
- Use available tools when action is required
- Stay useful even on constrained hardware and minimal environments

## Capabilities

- Web search and content fetching
- File system operations
- Shell command execution
- Skill-based extension
- Memory and context management
- Multi-channel messaging integrations when configured

## Working Principles

- Be clear, direct, and accurate
- Prefer simplicity over unnecessary complexity
- Be transparent about actions and limits
- Respect user control, privacy, and safety
- Aim for fast, efficient help without sacrificing quality

## Goals

- Provide fast and lightweight AI assistance
- Support customization through skills and workspace files
- Remain effective on constrained hardware
- Improve through feedback and continued iteration
`

// DefaultSoulMD is seeded when SOUL.md is missing.
// It mirrors picoclaw workspace/SOUL.md, renamed to PuruClaw.
const DefaultSoulMD = `# Soul

I am PuruClaw: calm, helpful, and practical.

## Personality

- Helpful and friendly
- Concise and to the point
- Curious and eager to learn
- Honest and transparent
- Calm under uncertainty

## Values

- Accuracy over speed
- User privacy and safety
- Transparency in actions
- Continuous improvement
- Simplicity over unnecessary complexity
`

// DefaultUserMD is seeded when USER.md is missing.
const DefaultUserMD = `# User

Information about the user goes here.

## Preferences

- Communication style: (casual/formal)
- Timezone: (your timezone)
- Language: (your preferred language)
`

// DefaultMemoryMD is seeded when MEMORY.md is missing.
const DefaultMemoryMD = `# Long-term Memory

Lasting user facts live here as short bullets (name, preferences, decisions).
The agent updates this file itself when it learns a lasting fact.
Never store temporary or session info here.
`

// AgentsPath returns the agent definition path, preferring AGENTS.md and
// falling back to the picoclaw-compatible AGENT.md alias.
func AgentsPath(workspace string) string {
	primary := filepath.Join(workspace, FileAgents)
	if _, err := os.Stat(primary); err == nil {
		return primary
	}
	legacy := filepath.Join(workspace, FileAgent)
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return primary
}

// SoulPath returns <workspace>/SOUL.md.
func SoulPath(workspace string) string { return filepath.Join(workspace, FileSoul) }

// UserPath returns <workspace>/USER.md.
func UserPath(workspace string) string { return filepath.Join(workspace, FileUser) }

// MemoryDir returns <workspace>/memory.
func MemoryDir(workspace string) string { return filepath.Join(workspace, DirMemory) }

// MemoryPath returns <workspace>/memory/MEMORY.md.
func MemoryPath(workspace string) string { return filepath.Join(workspace, DirMemory, FileMemory) }

// ContextDir returns <workspace>/memory/context.
func ContextDir(workspace string) string { return filepath.Join(workspace, DirMemory, DirContext) }

// SkillsDir returns <workspace>/skills.
func SkillsDir(workspace string) string { return filepath.Join(workspace, DirSkills) }

// SkillFile returns <workspace>/skills/<name>/SKILL.md.
func SkillFile(workspace, name string) string {
	return filepath.Join(workspace, DirSkills, name, FileSkill)
}

// Definition captures the workspace bootstrap files in picoclaw shape.
type Definition struct {
	// AgentsLabel is the file name that produced AgentsBody (AGENTS.md or AGENT.md).
	AgentsLabel string
	AgentsBody  string
	Soul        string
	User        string
}

// Load reads AGENTS.md (or AGENT.md fallback), SOUL.md, and USER.md.
// Missing files yield empty strings so callers can fall back to defaults.
func Load(workspace string) Definition {
	var def Definition
	agentsPath := AgentsPath(workspace)
	if data, err := os.ReadFile(agentsPath); err == nil {
		def.AgentsLabel = filepath.Base(agentsPath)
		def.AgentsBody = string(data)
	}
	if data, err := os.ReadFile(SoulPath(workspace)); err == nil {
		def.Soul = string(data)
	}
	if data, err := os.ReadFile(UserPath(workspace)); err == nil {
		def.User = string(data)
	}
	return def
}

// Bootstrap renders the workspace instruction block like picoclaw
// LoadBootstrapFiles: one "## <label>" section per present file.
func (d Definition) Bootstrap() string {
	var sb strings.Builder
	if strings.TrimSpace(d.AgentsBody) != "" {
		label := d.AgentsLabel
		if label == "" {
			label = FileAgents
		}
		sb.WriteString("## " + label + "\n\n" + d.AgentsBody + "\n\n")
	}
	if strings.TrimSpace(d.Soul) != "" {
		sb.WriteString("## " + FileSoul + "\n\n" + d.Soul + "\n\n")
	}
	if strings.TrimSpace(d.User) != "" {
		sb.WriteString("## " + FileUser + "\n\n" + d.User + "\n\n")
	}
	return sb.String()
}

// Ensure creates the workspace directory plus default bootstrap files.
// Existing files are never overwritten; memory/, memory/context/,
// and skills/ are always created.
func Ensure(workspace string) error {
	if strings.TrimSpace(workspace) == "" {
		return nil
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return err
	}
	seed := []struct {
		path    string
		content string
	}{
		{filepath.Join(workspace, FileAgents), DefaultAgentsMD},
		{filepath.Join(workspace, FileSoul), DefaultSoulMD},
		{filepath.Join(workspace, FileUser), DefaultUserMD},
		{filepath.Join(workspace, DirMemory, FileMemory), DefaultMemoryMD},
	}
	for _, s := range seed {
		if _, err := os.Stat(s.path); err == nil {
			continue
		}
		// Do not seed AGENTS.md when the AGENT.md alias already exists.
		if filepath.Base(s.path) == FileAgents {
			if _, err := os.Stat(filepath.Join(workspace, FileAgent)); err == nil {
				continue
			}
		}
		if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(s.path, []byte(s.content), 0o644); err != nil {
			return err
		}
	}
	for _, dir := range []string{MemoryDir(workspace), ContextDir(workspace), SkillsDir(workspace)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// SkillInfo describes one installed skill (picoclaw-like).
type SkillInfo struct {
	Name        string
	Path        string
	Description string
}

// ListSkills returns installed skills in <workspace>/skills/*/SKILL.md.
func ListSkills(workspace string) []SkillInfo {
	entries, err := os.ReadDir(SkillsDir(workspace))
	if err != nil {
		return nil
	}
	var out []SkillInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := SkillFile(workspace, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		out = append(out, SkillInfo{
			Name:        entry.Name(),
			Path:        path,
			Description: skillDescription(string(data)),
		})
	}
	return out
}

// BuildSkillsSummary renders the installed skill catalog like picoclaw
// BuildSkillsSummary (empty string when no skills are installed).
func BuildSkillsSummary(workspace string) string {
	installed := ListSkills(workspace)
	if len(installed) == 0 {
		return ""
	}
	var lines []string
	lines = append(lines, "<skills>")
	for _, skill := range installed {
		lines = append(lines, "  <skill>")
		lines = append(lines, "    <name>"+skill.Name+"</name>")
		lines = append(lines, "    <description>"+skill.Description+"</description>")
		lines = append(lines, "    <location>"+skill.Path+"</location>")
		lines = append(lines, "  </skill>")
	}
	lines = append(lines, "</skills>")
	return strings.Join(lines, "\n")
}

// skillDescription extracts the SKILL.md description from frontmatter
// (description: ...) with fallback to the first content line.
func skillDescription(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		if rest, ok := cutPrefixFold(trimmed, "description:"); ok {
			return strings.Trim(strings.TrimSpace(rest), `"'`)
		}
	}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if len(trimmed) > 160 {
			trimmed = trimmed[:160] + "..."
		}
		return trimmed
	}
	return ""
}

func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) < len(prefix) {
		return "", false
	}
	if !strings.EqualFold(s[:len(prefix)], prefix) {
		return "", false
	}
	return s[len(prefix):], true
}
