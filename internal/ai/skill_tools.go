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
// Detection helper only: file tools no longer block these paths since
// direct edits of active skills are allowed (they take effect next turn).
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

// rejectActiveSkillExec detects shell commands that would modify an active
// skill tree. Detection helper only: exec no longer blocks these commands
// since direct edits of active skills are allowed. Read-only inspection
// (ls, cat) was always allowed.
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
//
// use_skill modes (always_active bool, default false):
//   - false (once): load SKILL.md for this turn only, never persist.
//     When the turn ends with the final answer the skill idles automatically,
//     no stop_skill needed.
//   - true: persist via skillstate, injected as Active Skills every turn
//     until stop_skill.
func buildSkillTools(a *Agent, opts *ProcessOptions, mk func(string, func(context.Context, map[string]any) (any, error)) *Tool, errVal func(error) (any, error)) map[string]*Tool {
	useSkill := mk("use_skill",
		func(ctx context.Context, args map[string]any) (any, error) {
			name := strings.TrimSpace(argStr(args, "name"))
			if name == "" {
				return errVal(outputErr("use_skill.name_required"))
			}
			persist := argBool(args, "always_active")
			ws := ""
			if a != nil && a.Config != nil {
				ws = a.Config.Workspace
			}
			if strings.TrimSpace(ws) == "" {
				return errVal(outputErr("use_skill.unavailable"))
			}
			canonical, ok := workspace.ResolveSkillName(ws, name)
			if !ok {
				return errVal(outputErr("use_skill.unknown", name))
			}
			if a != nil && a.Config != nil && !a.Config.SkillsPolicy().Allows(canonical) {
				return errVal(outputErr("use_skill.blocked", canonical))
			}
			body, ok := workspace.LoadSkill(ws, canonical)
			if !ok {
				return errVal(outputErr("use_skill.no_body", canonical))
			}
			if strings.TrimSpace(body) == "" {
				body = outputText("use_skill.empty_body")
			}
			if !persist {
				// Once mode (default): load for this turn only, never
				// persist. When the turn ends with the final answer the
				// skill idles automatically, no stop_skill needed.
				return outputf("use_skill.loaded_once", canonical, body), nil
			}
			chatID := int64(0)
			if opts != nil {
				chatID = opts.ChatID
			}
			if chatID == 0 {
				// No chat scope (unit tests): return the body without persisting.
				return outputf("use_skill.loaded_no_chat", canonical, body), nil
			}
			store := skillStateFor(a)
			if store == nil {
				return outputf("use_skill.loaded_plain", canonical, body), nil
			}
			added, err := store.Activate(chatID, canonical)
			if err != nil {
				return errVal(outputErr("use_skill.persist_failed", err))
			}
			if !added {
				return outputf("use_skill.already_active", canonical, body), nil
			}
			return outputf("use_skill.activated", canonical, body), nil
		})
	stopSkill := mk("stop_skill",
		func(ctx context.Context, args map[string]any) (any, error) {
			name := strings.TrimSpace(argStr(args, "name"))
			if name == "" {
				return errVal(outputErr("stop_skill.name_required"))
			}
			chatID := int64(0)
			if opts != nil {
				chatID = opts.ChatID
			}
			if chatID == 0 {
				return errVal(outputErr("stop_skill.no_chat_scope"))
			}
			store := skillStateFor(a)
			if store == nil {
				return errVal(outputErr("stop_skill.unavailable"))
			}
			removed, err := store.Deactivate(chatID, name)
			if err != nil {
				return errVal(outputErr("use_skill.persist_failed", err))
			}
			if !removed {
				return outputf("stop_skill.not_active", name), nil
			}
			return outputf("stop_skill.deactivated", name), nil
		})
	return map[string]*Tool{"use_skill": useSkill, "stop_skill": stopSkill}
}
