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
	"embed"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// builtinSkills embeds the skills shipped with the binary, seeded into
// <workspace>/skills/ on first run.
//
//go:embed skills
var builtinSkills embed.FS

// DefaultAgentsMD is seeded when neither AGENTS.md nor AGENT.md exists.
//
//go:embed defaults/AGENTS.md
var DefaultAgentsMD string

// DefaultSoulMD is seeded when SOUL.md is missing.
//
//go:embed defaults/SOUL.md
var DefaultSoulMD string

// DefaultUserMD is seeded when USER.md is missing.
//
//go:embed defaults/USER.md
var DefaultUserMD string

// DefaultMemoryMD is seeded when MEMORY.md is missing.
//
//go:embed defaults/MEMORY.md
var DefaultMemoryMD string

var skillNamePattern = regexp.MustCompile(`^[a-zA-Z0-9]+(-[a-zA-Z0-9]+)*$`)

// Skill injection modes, picoclaw turn_profile.skills-like.
const (
	// SkillsModeDefault injects the full catalog plus frontmatter active skills.
	SkillsModeDefault = "default"
	// SkillsModeOff suppresses every skill section in the system prompt.
	SkillsModeOff = "off"
	// SkillsModeCustom injects only allowlisted skills.
	SkillsModeCustom = "custom"
)

// SkillsPolicy controls which skills reach the system prompt.
type SkillsPolicy struct {
	Mode  string
	Allow []string
}

// NormalizeSkillsMode trims and lowercases a mode, defaulting empty to default.
func NormalizeSkillsMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", SkillsModeDefault:
		return SkillsModeDefault
	case SkillsModeOff:
		return SkillsModeOff
	case SkillsModeCustom:
		return SkillsModeCustom
	default:
		return strings.ToLower(strings.TrimSpace(mode))
	}
}

// Allows reports whether a skill may be injected under this policy.
// Direct file reads (LoadSkill) are never gated, picoclaw-like.
func (p SkillsPolicy) Allows(name string) bool {
	switch NormalizeSkillsMode(p.Mode) {
	case SkillsModeOff:
		return false
	case SkillsModeCustom:
		for _, allowed := range p.Allow {
			if strings.EqualFold(strings.TrimSpace(allowed), strings.TrimSpace(name)) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

// FilterSkills keeps only policy-allowed skills.
func FilterSkills(skills []SkillInfo, policy SkillsPolicy) []SkillInfo {
	var out []SkillInfo
	for _, skill := range skills {
		if policy.Allows(skill.Name) {
			out = append(out, skill)
		}
	}
	return out
}

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
)

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
	// AgentsBody is the agent definition without YAML frontmatter.
	AgentsBody string
	// FrontmatterSkills lists active skills from the agent definition
	// frontmatter (skills: [...]), picoclaw-like SkillsFilter.
	FrontmatterSkills []string
	Soul              string
	User              string
}

// Load reads AGENTS.md (or AGENT.md fallback), SOUL.md, and USER.md.
// Missing files yield empty strings so callers can fall back to defaults.
// A leading YAML frontmatter block in the agent file is stripped from
// AgentsBody and its skills: list is exposed via FrontmatterSkills.
func Load(workspace string) Definition {
	var def Definition
	agentsPath := AgentsPath(workspace)
	if data, err := os.ReadFile(agentsPath); err == nil {
		def.AgentsLabel = filepath.Base(agentsPath)
		frontmatter, body := splitFrontmatter(string(data))
		def.AgentsBody = body
		def.FrontmatterSkills = parseSkillsList(frontmatter)
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
	return seedBuiltinSkills(workspace)
}

// seedBuiltinSkills copies embedded builtin skills into <workspace>/skills/.
// Existing skill directories are never touched.
func seedBuiltinSkills(workspace string) error {
	entries, err := builtinSkills.ReadDir("skills")
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		target := filepath.Join(SkillsDir(workspace), entry.Name(), FileSkill)
		if _, err := os.Stat(target); err == nil {
			continue
		}
		data, err := builtinSkills.ReadFile("skills/" + entry.Name() + "/" + FileSkill)
		if err != nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// MergeActiveSkills merges frontmatter and runtime skill lists,
// deduping case-insensitively while preserving first-seen order.
func MergeActiveSkills(lists ...[]string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, list := range lists {
		for _, name := range list {
			trimmed := strings.TrimSpace(name)
			if trimmed == "" {
				continue
			}
			folded := strings.ToLower(trimmed)
			if _, ok := seen[folded]; ok {
				continue
			}
			seen[folded] = struct{}{}
			out = append(out, trimmed)
		}
	}
	return out
}

// SkillNameFromPath extracts the skill name when p points inside
// <workspace>/skills/<name>/... (or the relative skills/<name>/... form).
// It reports false for paths outside the skills tree.
func SkillNameFromPath(workspacePath, p string) (string, bool) {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" {
		return "", false
	}
	cleaned := filepath.Clean(filepath.FromSlash(trimmed))
	if filepath.IsAbs(cleaned) && strings.TrimSpace(workspacePath) != "" {
		rel, err := filepath.Rel(strings.TrimSpace(workspacePath), cleaned)
		if err != nil {
			return "", false
		}
		cleaned = rel
	}
	parts := strings.Split(cleaned, string(os.PathSeparator))
	if len(parts) < 2 || parts[0] != DirSkills {
		return "", false
	}
	name := strings.TrimSpace(parts[1])
	if name == "" || name == "." || name == ".." {
		return "", false
	}
	return name, true
}

// SkillInfo describes one installed skill (picoclaw-like).
type SkillInfo struct {
	Name        string
	Path        string
	Source      string
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
		name, description := skillMetadata(entry.Name(), string(data))
		if !skillNamePattern.MatchString(name) || len(name) > 64 {
			continue
		}
		if len(description) > 1024 {
			description = description[:1024]
		}
		out = append(out, SkillInfo{
			Name:        name,
			Path:        path,
			Source:      "workspace",
			Description: description,
		})
	}
	return out
}

// ResolveSkillName returns the canonical installed skill name,
// matching case-insensitively (picoclaw-like).
func ResolveSkillName(workspace, name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	for _, skill := range ListSkills(workspace) {
		if strings.EqualFold(skill.Name, name) {
			return skill.Name, true
		}
	}
	return "", false
}

// LoadSkill returns the SKILL.md body for one installed skill with the
// YAML frontmatter stripped, or false when the skill is not installed.
func LoadSkill(workspace, name string) (string, bool) {
	canonical, ok := ResolveSkillName(workspace, name)
	if !ok {
		return "", false
	}
	data, err := os.ReadFile(SkillFile(workspace, canonical))
	if err != nil {
		return "", false
	}
	_, body := splitFrontmatter(string(data))
	return body, true
}

// LoadSkillsForContext renders full skill bodies for active skills
// (picoclaw-like LoadSkillsForContext), skipping policy-blocked and
// unknown names.
func LoadSkillsForContext(workspace string, names []string, policy SkillsPolicy) string {
	if len(names) == 0 {
		return ""
	}
	var parts []string
	for _, name := range names {
		if !policy.Allows(name) {
			continue
		}
		content, ok := LoadSkill(workspace, name)
		if ok {
			parts = append(parts, "### Skill: "+name+"\n\n"+content)
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// BuildSkillsSummary renders the installed skill catalog like picoclaw
// BuildSkillsSummary (empty string when no skills are installed or the
// policy blocks them all).
func BuildSkillsSummary(workspace string, policy SkillsPolicy) string {
	return BuildSkillsSummaryExcluding(workspace, policy, nil)
}

// BuildSkillsSummaryExcluding renders the catalog without the excluded
// (already active) skills so the model is not told to load them twice.
// The catalog stays cheap: name + description only, never skill bodies.
func BuildSkillsSummaryExcluding(workspace string, policy SkillsPolicy, exclude []string) string {
	installed := FilterSkills(ListSkills(workspace), policy)
	if len(exclude) > 0 {
		skip := map[string]struct{}{}
		for _, name := range exclude {
			trimmed := strings.TrimSpace(name)
			if trimmed != "" {
				skip[strings.ToLower(trimmed)] = struct{}{}
			}
		}
		kept := make([]SkillInfo, 0, len(installed))
		for _, skill := range installed {
			if _, ok := skip[strings.ToLower(skill.Name)]; !ok {
				kept = append(kept, skill)
			}
		}
		installed = kept
	}
	if len(installed) == 0 {
		return ""
	}
	var lines []string
	lines = append(lines, "<skills>")
	for _, skill := range installed {
		lines = append(lines, "  <skill>")
		lines = append(lines, "    <name>"+escapeXML(skill.Name)+"</name>")
		lines = append(lines, "    <description>"+escapeXML(skill.Description)+"</description>")
		lines = append(lines, "    <location>"+escapeXML(skill.Path)+"</location>")
		lines = append(lines, "    <source>"+escapeXML(skill.Source)+"</source>")
		lines = append(lines, "  </skill>")
	}
	lines = append(lines, "</skills>")
	// fence xml so GitHub/text extractors don't strip tags into one blob
	return "```xml\n" + strings.Join(lines, "\n") + "\n```"
}

func escapeXML(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(s)
}

// skillMetadata extracts the SKILL.md name and description from YAML
// frontmatter (name: or slug: alias, description:), ignoring all unknown
// fields (version:, metadata:, etc). It falls back to the directory name
// and the first body line.
func skillMetadata(dirName, content string) (name, description string) {
	frontmatter, body := splitFrontmatter(content)
	name = dirName
	slug := ""
	description = ""
	for _, line := range strings.Split(frontmatter, "\n") {
		trimmed := strings.TrimSpace(line)
		if rest, ok := cutPrefixFold(trimmed, "name:"); ok && strings.TrimSpace(rest) != "" {
			name = strings.Trim(strings.TrimSpace(rest), `"'`)
		}
		if rest, ok := cutPrefixFold(trimmed, "slug:"); ok && strings.TrimSpace(rest) != "" {
			slug = strings.Trim(strings.TrimSpace(rest), `"'`)
		}
		if rest, ok := cutPrefixFold(trimmed, "description:"); ok && strings.TrimSpace(rest) != "" {
			description = strings.Trim(strings.TrimSpace(rest), `"'`)
		}
	}
	// Clawic-style frontmatter uses slug: instead of name:.
	if strings.EqualFold(name, dirName) && slug != "" {
		name = slug
	}
	if description != "" {
		return name, description
	}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if len(trimmed) > 160 {
			trimmed = trimmed[:160] + "..."
		}
		return name, trimmed
	}
	return name, ""
}

// splitFrontmatter splits a leading "---" YAML frontmatter block from the
// body, returning ("", content) when no block is present. Both "---" and
// "..." close the block; unknown fields inside are left untouched for the
// caller to ignore.
func splitFrontmatter(content string) (frontmatter, body string) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", content
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "---" || trimmed == "..." {
			end = i
			break
		}
	}
	if end == -1 {
		return "", content
	}
	return strings.Join(lines[1:end], "\n"), strings.TrimLeft(strings.Join(lines[end+1:], "\n"), "\n")
}

// parseSkillsList parses a skills: list from YAML frontmatter, supporting
// flow style (skills: [a, b]) and block style (- a). Unknown or missing
// keys yield nil.
func parseSkillsList(frontmatter string) []string {
	var out []string
	inBlock := false
	flush := func(items []string) {
		for _, item := range items {
			item = strings.Trim(strings.TrimSpace(item), `"'`)
			if item == "" {
				continue
			}
			dup := false
			for _, existing := range out {
				if strings.EqualFold(existing, item) {
					dup = true
					break
				}
			}
			if !dup {
				out = append(out, item)
			}
		}
	}
	for _, line := range strings.Split(frontmatter, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inBlock {
			rest, ok := cutPrefixFold(trimmed, "skills:")
			if !ok {
				continue
			}
			rest = strings.TrimSpace(rest)
			if strings.HasPrefix(rest, "[") {
				flush(strings.Split(strings.Trim(rest, "[]"), ","))
				continue
			}
			if rest != "" {
				flush([]string{rest})
				continue
			}
			inBlock = true
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || trimmed == "-" {
			flush([]string{strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))})
			continue
		}
		if trimmed == "" {
			continue
		}
		inBlock = false
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
