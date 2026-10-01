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
	return mk("spawn_agent", "Spawn a synchronous sub-agent for one delegated task. Use when a task is independent and needs its own system prompt and limited tools (e.g. researcher, scraper). Runs inline (not in background) with the same model and max_iterations from config, then returns its final answer directly as \"Result from <agent_name>: ...\" (or a step-limit notice when capped). Do NOT use for trivial tasks you can do yourself. Capabilities are gated by four combinable flags: agent_read, agent_write, agent_exec, agent_search; all false/omitted = all parent tools except spawn_agent. Skill tools (use_skill, stop_skill) are always available. Notes: the child cannot spawn again (spawn_agent is stripped) and cannot use your chat or Telegram tools directly; each spawn costs model time and shares your step budget, so batch tightly. Returns {success:false,error} when no model is configured, required args are empty, the flags match no tools, or the child errors.",
		objSchema([]string{"agent_name", "system_prompt", "task_prompt"}, map[string]any{
			"agent_name":    strProp("Sub-agent name, non-empty. Example: researcher_01."),
			"system_prompt": strProp("System prompt for the sub-agent: its role, rules, and output shape. Non-empty. Example: You are a researcher. Return 3 bullets with sources."),
			"task_prompt":   strProp("Task for the sub-agent to execute. Non-empty. Example: Summarize docs/api.md in 5 bullets."),
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
			subSystem := fmt.Sprintf(`%s

You are sub-agent %q, spawned by the parent agent to do one delegated task.

<role>
Your job is to finish exactly the task in task_prompt and hand the parent a usable result, not a chatty report.
</role>

<instructions>
1. Do the task with the tools you were given — no more, no less.
2. Return the final result directly, concisely, in the shape your system prompt asks for.
3. Do not ask the parent questions; decide and state your assumption instead.
</instructions>

<constraints>
- Stay inside the workspace (paths outside it are rejected) — this keeps you from touching files outside the project.
- Anything not in task_prompt is out of scope; note it in one line instead of doing it — this keeps the delegation tight and avoids wasted steps.
</constraints>

<format>
Plain result text. No preamble, no restating the task, no offer of further help.
</format>

<fallbacks>
- If a tool fails, retry once with corrected arguments. If it fails again, return what you have plus the exact error.
- If required information is missing, say what is missing and stop.
- If you run out of steps, return your partial result and label it partial.
</fallbacks>

<example>
task_prompt: "Summarize notes.txt in 3 bullets."
Good return: three bullets, one per point, no header.
</example>`, system, name)
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
