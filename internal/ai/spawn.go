// Sub-agent spawn tool (synchronous, no background).
//
// spawn_agent runs a child agent inline with the same model + config and
// returns its final answer directly to the parent. The child shares the
// parent's max_iterations budget rule (a.maxSteps) and loop delay, so no
// extra config key is needed. Recursion is cut by stripping spawn_agent
// from the child's toolbox.
package ai

import (
	"context"
	"fmt"
	"strings"
)

func buildSpawnTool(a *Agent, opts *ProcessOptions, mk func(string, string, map[string]any, func(context.Context, map[string]any) (any, error)) *Tool, errVal func(error) (any, error)) *Tool {
	return mk("spawn_agent", "Spawn a synchronous sub-agent for one delegated task. Use when a task is independent and needs its own system prompt and limited tools (e.g. researcher, scraper). Runs inline (not in background) with the same model and max_iterations from config, then returns its final answer directly. Do NOT use for trivial tasks you can do yourself. Capabilities are gated by four combinable flags: agent_read, agent_write, agent_exec, agent_search. All false/omitted = all parent tools except spawn_agent. Skill tools (use_skill, stop_skill) are always available. The sub-agent cannot spawn further sub-agents.",
		objSchema([]string{"agent_name", "system_prompt", "task_prompt"}, map[string]any{
			"agent_name":    strProp("Sub-agent name, e.g. 'researcher_01'. Required, non-empty."),
			"system_prompt": strProp("System prompt for the sub-agent (its role and rules). Required, non-empty."),
			"task_prompt":   strProp("Task for the sub-agent to execute. Required, non-empty."),
			"agent_read":    boolProp("Read-only tools: read_file, list_dir, get_env, telegram_getuser. Combinable with other agent_* flags.", false),
			"agent_write":   boolProp("Write tools: write_file, edit_file, append_file, telegram_sendfile. Combinable with other agent_* flags.", false),
			"agent_exec":    boolProp("Execution tools: exec, schedule. Combinable with other agent_* flags.", false),
			"agent_search":  boolProp("Search/fetch tools: web_fetch, web_search (when available). Combinable with other agent_* flags.", false),
		}),
		func(ctx context.Context, args map[string]any) (any, error) {
			if a == nil || a.Client == nil {
				return errVal(fmt.Errorf("spawn_agent unavailable: no AI model configured"))
			}
			name := strings.TrimSpace(argStr(args, "agent_name"))
			system := strings.TrimSpace(argStr(args, "system_prompt"))
			task := strings.TrimSpace(argStr(args, "task_prompt"))
			if name == "" {
				return errVal(fmt.Errorf("agent_name is required"))
			}
			if task == "" {
				return errVal(fmt.Errorf("task_prompt is required"))
			}
			if system == "" {
				return errVal(fmt.Errorf("system_prompt is required"))
			}
			full, terr := a.toolsFor(opts)
			if terr != nil || len(full) == 0 {
				return errVal(fmt.Errorf("spawn_agent unavailable: cannot build tools"))
			}
			subTools := filterSpawnTools(full, args)
			if len(subTools) == 0 {
				return errVal(fmt.Errorf("agent_* flags matched no tools"))
			}
			subSystem := fmt.Sprintf("%s\n\nYou are sub-agent %q spawned by the parent agent. Complete the task and return the final result directly, concisely.", system, name)
			run, rerr := a.runOnce(ctx, subSystem, nil, task, opts, subTools)
			if rerr != nil {
				return errVal(rerr)
			}
			if run.hitStepLimit {
				if strings.TrimSpace(run.finalText) != "" {
					return fmt.Sprintf("Sub-agent %q hit step limit (max_iterations=%d). Partial result:\n%s", name, a.maxSteps(), run.finalText), nil
				}
				return fmt.Sprintf("Sub-agent %q hit step limit (max_iterations=%d) with no final answer.", name, a.maxSteps()), nil
			}
			if strings.TrimSpace(run.finalText) == "" {
				return errVal(fmt.Errorf("sub-agent %q returned empty result", name))
			}
			return fmt.Sprintf("Result from %q:\n%s", name, run.finalText), nil
		})
}

// filterSpawnTools restricts the parent toolbox to the four agent_* flags.
// All false/missing = all except spawn_agent (no nesting).
// Each true flag adds its group (union, combinable); skill tools
// (use_skill, stop_skill) are always included when filtering.
func filterSpawnTools(full map[string]*Tool, args map[string]any) map[string]*Tool {
	out := map[string]*Tool{}
	if full == nil {
		return out
	}
	lower := map[string]*Tool{}
	for n, t := range full {
		if strings.EqualFold(n, "spawn_agent") {
			continue
		}
		lower[strings.ToLower(n)] = t
	}
	add := func(names ...string) {
		for _, n := range names {
			if t, ok := lower[strings.ToLower(n)]; ok {
				out[t.Name] = t
			}
		}
	}
	if args == nil {
		for _, t := range lower {
			out[t.Name] = t
		}
		return out
	}
	rd := argBool(args, "agent_read")
	wr := argBool(args, "agent_write")
	ex := argBool(args, "agent_exec")
	se := argBool(args, "agent_search")
	if !rd && !wr && !ex && !se {
		for _, t := range lower {
			out[t.Name] = t
		}
		return out
	}
	// Skill tools always available so sub-agents can use loaded skills.
	add("use_skill", "stop_skill")
	if rd {
		add("read_file", "list_dir", "get_env", "telegram_getuser")
	}
	if wr {
		add("write_file", "edit_file", "append_file", "telegram_sendfile")
	}
	if ex {
		add("exec", "schedule")
	}
	if se {
		add("web_fetch", "web_search")
	}
	return out
}
