// Skill activation tools (use_skill / stop_skill) with persistent per-chat state.
//
// The system prompt only carries the cheap skill catalog (name + description).
// Full SKILL.md bodies are injected as Active Skills only for frontmatter
// defaults plus runtime-active skills tracked in ~/.puru/skillstate/.
// Only names are persisted, never bodies, so SKILL.md edits take effect
// on the next prompt build without re-activation.
package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/purujawa06-bot/PURU-AI/internal/skillstate"
	"github.com/purujawa06-bot/PURU-AI/internal/workspace"
)

// skillStateFor returns the per-chat active-skill store, or nil when the
// workspace config is missing (CLI tests, nil agent).
func skillStateFor(a *Agent) *skillstate.Store {
	if a == nil || a.Config == nil {
		return nil
	}
	dir := strings.TrimSpace(a.Config.SkillStateDir())
	if dir == "" {
		return nil
	}
	return skillstate.New(dir)
}

// ActiveSkillsFor loads runtime-active names for this chat.
// Missing skills or policy blocks are filtered at prompt build time by
// LoadSkillsForContext, so no disk scan happens here: this stays cheap
// on every turn. Stale entries are pruned on use/stop, never fail the turn.
func ActiveSkillsFor(a *Agent, opts *ProcessOptions) []string {
	if a == nil || opts == nil || opts.ChatID == 0 {
		return nil
	}
	store := skillStateFor(a)
	if store == nil {
		return nil
	}
	return store.Get(opts.ChatID)
}

// effectiveActiveSkills reports the merged frontmatter + runtime set used for
// guards, so file tools block edits to either kind of active skill.
func effectiveActiveSkills(a *Agent, opts *ProcessOptions) []string {
	ws := ""
	if a != nil && a.Config != nil {
		ws = a.Config.Workspace
	}
	var frontmatter []string
	if strings.TrimSpace(ws) != "" {
		frontmatter = workspace.Load(ws).FrontmatterSkills
	}
	return workspace.MergeActiveSkills(frontmatter, ActiveSkillsFor(a, opts))
}

// isActiveSkillPath reports whether p points inside skills/<name>/... where
// name is currently active (frontmatter or runtime) for this chat.
func isActiveSkillPath(a *Agent, opts *ProcessOptions, p string) (string, bool) {
	ws := ""
	if a != nil && a.Config != nil {
		ws = a.Config.Workspace
	}
	name, ok := workspace.SkillNameFromPath(ws, p)
	if !ok {
		return "", false
	}
	for _, active := range effectiveActiveSkills(a, opts) {
		if strings.EqualFold(active, name) {
			canonical, ok := workspace.ResolveSkillName(ws, name)
			if !ok {
				canonical = name
			}
			return canonical, true
		}
	}
	return "", false
}

// rejectActiveSkillExec blocks shell commands that would modify an active
// skill tree. Read-only inspection (ls, cat, grep) stays allowed; destructive
// or write-like commands referencing skills/<active>/ are rejected with a
// stop_skill hint. This is heuristic, the file-tool guard is authoritative.
func rejectActiveSkillExec(a *Agent, opts *ProcessOptions, command string) (string, bool) {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return "", false
	}
	lowered := strings.ToLower(cmd)
	writeTokens := []string{"rm ", "rmdir", "del ", "erase ", "mv ", "cp ", "echo ", "printf ", "tee ", "truncate", "sed ", "awk ", ">", "unlink"}
	hasWrite := false
	for _, token := range writeTokens {
		if strings.Contains(lowered, strings.ToLower(token)) {
			hasWrite = true
			break
		}
	}
	if !hasWrite {
		return "", false
	}
	for _, active := range effectiveActiveSkills(a, opts) {
		folded := strings.ToLower(strings.TrimSpace(active))
		if folded == "" {
			continue
		}
		if strings.Contains(lowered, "skills/"+folded) {
			return active, true
		}
	}
	return "", false
}

// buildSkillTools returns the use_skill / stop_skill tools sharing the same
// mk/errVal helpers as the file tools.
func buildSkillTools(a *Agent, opts *ProcessOptions, mk func(string, string, map[string]any, func(context.Context, map[string]any) (any, error)) *Tool, errVal func(error) (any, error)) map[string]*Tool {
	useSkill := mk("use_skill", "Activate an installed skill by name. The full SKILL.md body loads automatically into Active Skills on this and following turns. Use for skills listed in the <skills> catalog; already-active skills need no call.",
		objSchema([]string{"name"}, map[string]any{
			"name": strProp("Exact skill <name> from the <skills> catalog (case-insensitive)."),
		}),
		func(ctx context.Context, args map[string]any) (any, error) {
			name := strings.TrimSpace(argStr(args, "name"))
			if name == "" {
				return errVal(fmt.Errorf("name is required (exact <name> from the <skills> catalog)"))
			}
			ws := ""
			if a != nil && a.Config != nil {
				ws = a.Config.Workspace
			}
			if strings.TrimSpace(ws) == "" {
				return errVal(fmt.Errorf("use_skill unavailable: workspace not configured"))
			}
			canonical, ok := workspace.ResolveSkillName(ws, name)
			if !ok {
				return errVal(fmt.Errorf("unknown skill %q (check the <skills> catalog)", name))
			}
			if a != nil && a.Config != nil && !a.Config.SkillsPolicy().Allows(canonical) {
				return errVal(fmt.Errorf("skill %q is blocked by skills policy", canonical))
			}
			body, ok := workspace.LoadSkill(ws, canonical)
			if !ok || strings.TrimSpace(body) == "" {
				return errVal(fmt.Errorf("skill %q has no readable SKILL.md", canonical))
			}
			chatID := int64(0)
			if opts != nil {
				chatID = opts.ChatID
			}
			if chatID == 0 {
				// No chat scope (unit tests): return the body without persisting.
				return fmt.Sprintf("Skill %q loaded for this turn only (no chat scope to persist).\n\n%s", canonical, body), nil
			}
			store := skillStateFor(a)
			if store == nil {
				return fmt.Sprintf("Skill %q loaded for this turn only.\n\n%s", canonical, body), nil
			}
			added, err := store.Activate(chatID, canonical)
			if err != nil {
				return errVal(fmt.Errorf("persist active skill: %w", err))
			}
			if !added {
				return fmt.Sprintf("Skill %q is already active.\n\n%s", canonical, body), nil
			}
			return fmt.Sprintf("Skill %q activated. Its full body is now injected as Active Skills and stays active until stop_skill.\n\n%s", canonical, body), nil
		})
	stopSkill := mk("stop_skill", "Deactivate a runtime-active skill so its body is no longer injected. Frontmatter defaults stay active; only runtime use_skill state is removed.",
		objSchema([]string{"name"}, map[string]any{
			"name": strProp("Active skill name to deactivate (case-insensitive)."),
		}),
		func(ctx context.Context, args map[string]any) (any, error) {
			name := strings.TrimSpace(argStr(args, "name"))
			if name == "" {
				return errVal(fmt.Errorf("name is required"))
			}
			chatID := int64(0)
			if opts != nil {
				chatID = opts.ChatID
			}
			if chatID == 0 {
				return errVal(fmt.Errorf("stop_skill unavailable without chat scope"))
			}
			store := skillStateFor(a)
			if store == nil {
				return errVal(fmt.Errorf("stop_skill unavailable: workspace not configured"))
			}
			removed, err := store.Deactivate(chatID, name)
			if err != nil {
				return errVal(fmt.Errorf("persist active skill: %w", err))
			}
			if !removed {
				return fmt.Sprintf("Skill %q is not runtime-active (frontmatter defaults, if any, stay active).", name), nil
			}
			return fmt.Sprintf("Skill %q deactivated. It returns to the <skills> catalog and is no longer injected.", name), nil
		})
	return map[string]*Tool{"use_skill": useSkill, "stop_skill": stopSkill}
}
