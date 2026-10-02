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

func buildSpawnTool(a *Agent, opts *ProcessOptions, mk func(string, func(context.Context, map[string]any) (any, error)) *Tool, errVal func(error) (any, error)) *Tool {
	return mk("spawn_agent",
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
		add("read_file", "list_dir", "grep", "get_env", "telegram_getuser")
	}
	if wr {
		add("write_file", "edit_file", "append_file", "telegram_sendfile")
	}
	if ex {
		add("run_shell_command", "manage_schedule")
	}
	if se {
		add("web_fetch", "web_search")
	}
	return out
}
