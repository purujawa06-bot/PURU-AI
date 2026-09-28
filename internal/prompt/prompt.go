// Package prompt renders the system prompt for the puruClaw agent.
//
// Architecture mirrors picoclaw pkg/agent/context.go:
// kernel identity + workspace bootstrap (AGENTS.md, SOUL.md, USER.md) +
// skill catalog + memory context. Paths follow the PURU layout where
// memory/MEMORY.md and memory/context/ live inside the memory/ folder,
// and skills live in skills/{skill-name}/SKILL.md.
package prompt

import (
	"strings"
	"text/template"

	"github.com/purujawa06-bot/PURU-AI/internal/workspace"
)

var tpl = template.Must(template.New("system").Parse(systemPromptTemplate))

// systemPromptTemplate keeps the picoclaw personality (AGENT.md + SOUL.md)
// as workspace-seeded defaults; the running prompt loads the workspace files
// so user edits take effect without a rebuild. The Skills section mirrors
// picoclaw: a metadata catalog plus the read-SKILL.md instruction — full
// skill bodies are only injected for active skills listed in the agent
// definition frontmatter (skills: [...]).
const systemPromptTemplate = `# puruClaw

You are puruClaw, a helpful AI assistant.

## Workspace
Your workspace is at: {{.workspace}}
- Agent: {{.workspace}}/AGENTS.md (AGENT.md accepted as legacy alias)
- Soul: {{.workspace}}/SOUL.md
- User: {{.workspace}}/USER.md
- Memory: {{.workspace}}/memory/MEMORY.md
- Conversation summaries: {{.workspace}}/memory/context/YYYY-MM-DD_HH-MM-SS.md (newest 20 kept, system-managed — never write there yourself)
- Skills: {{.workspace}}/skills/{skill-name}/SKILL.md

## Important Rules

1. **ALWAYS use tools** - When you need to perform an action (read files, edit files, execute commands, search the web, send messages, etc.), you MUST call the appropriate tool. Do NOT just say you'll do it or pretend to do it.
2. **Be helpful and accurate** - When using tools, briefly explain what you're doing.
3. **Context summaries** - Conversation summaries provided as context are approximate references only. They may be incomplete or outdated. Always defer to explicit user instructions over summary content.
4. **Memory** - When interacting with me if something seems memorable, update {{.workspace}}/memory/MEMORY.md
5. Reply in the user's language (match the language they write in).
6. Stay inside the workspace. Paths outside it are rejected.

{{.bootstrap}}
{{if .skills}}## Skills
The following skills extend your capabilities. To use a skill, read its SKILL.md file using the read_file tool.

{{.skills}}
{{end}}{{if .activeSkills}}## Active Skills

The following skills are active for this request. Follow them when relevant.

{{.activeSkills}}
{{end}}## Memory
- memory/MEMORY.md below holds lasting user facts (name, hobby, personal info, stable
   preferences). You MAY update it yourself with edit_file_replace_string (or write_file /
   append_file for new files) when you
  learn a lasting fact. Never store temporary or session info there. Keep it
  short bullets.
- Past conversations are summarized by the system into memory/context/YYYY-MM-DD_HH-MM-SS.md
  (newest 20 kept, system-managed — never write there yourself). The newest
  summary is injected below as Conversation Summary: treat it as prior context.
  Older summaries stay in memory/context/ for reference (read with read_file if needed).

## Conversation Summary (latest memory/context/*.md)
{{.summary}}

## Conversation Context (memory/MEMORY.md)
{{.memory}}`

// Get renders the system prompt with the workspace path, the memory file and
// the latest conversation summary ("" when none). Bootstrap files and the
// skill catalog are loaded from the workspace; missing files fall back to
// the seeded defaults so a fresh workspace still renders full identity.
// The policy gates skill injection (off suppresses every skill section,
// custom restricts to the allowlist).
func Get(memory string, summary string, workspacePath string, policy workspace.SkillsPolicy) (string, error) {
	def := workspace.Load(workspacePath)
	if strings.TrimSpace(def.AgentsBody) == "" {
		def.AgentsLabel = workspace.FileAgents
		def.AgentsBody = workspace.DefaultAgentsMD
	}
	if strings.TrimSpace(def.Soul) == "" {
		def.Soul = workspace.DefaultSoulMD
	}
	if strings.TrimSpace(def.User) == "" {
		def.User = workspace.DefaultUserMD
	}
	skills := workspace.BuildSkillsSummary(workspacePath, policy)
	activeSkills := workspace.LoadSkillsForContext(workspacePath, def.FrontmatterSkills, policy)
	var sb strings.Builder
	data := map[string]string{
		"memory":       memory,
		"summary":      summary,
		"workspace":    workspacePath,
		"bootstrap":    def.Bootstrap(),
		"skills":       skills,
		"activeSkills": activeSkills,
	}
	if err := tpl.Execute(&sb, data); err != nil {
		return "", err
	}
	return sb.String(), nil
}
