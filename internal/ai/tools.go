package ai

import (
	"context"
	"fmt"
)

// BuildTools returns exactly 4 local tools. opts carries workspace config.
func BuildTools(a *Agent, opts *ProcessOptions) map[string]*Tool {
	ws := ""
	restrict := true
	if a != nil && a.Config != nil {
		ws = a.Config.Workspace
		restrict = a.Config.RestrictWorkspace
	}
	mk := func(name, desc string, params map[string]any, run func(ctx context.Context, args map[string]any) (any, error)) *Tool {
		return &Tool{Name: name, Description: desc, Parameters: params, Run: run}
	}
	_ = opts
	return map[string]*Tool{
		"read_file": mk("read_file", "Read a text file inside the workspace.",
			objSchema([]string{"path"}, map[string]any{
				"path": strProp("Workspace-relative path, e.g. \"main.go\" or \"notes/todo.md\"."),
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				content, err := readLocalFile(ws, restrict, argStr(args, "path"))
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				return map[string]any{"content": content, "path": argStr(args, "path")}, nil
			}),
		"write_file": mk("write_file", "Create or overwrite a text file inside the workspace (parent dirs auto-created).",
			objSchema([]string{"path", "content"}, map[string]any{
				"path":    strProp("Workspace-relative path to write."),
				"content": strProp("Full text content to write."),
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				if err := writeLocalFile(ws, restrict, argStr(args, "path"), argStr(args, "content")); err != nil {
					return map[string]any{"success": false, "error": err.Error()}, nil
				}
				return map[string]any{"success": true, "path": argStr(args, "path")}, nil
			}),
		"edit_file": mk("edit_file", "Replace one unique old_string with new_string in a workspace file.",
			objSchema([]string{"path", "old_string", "new_string"}, map[string]any{
				"path":       strProp("Workspace-relative path."),
				"old_string": strProp("Exact text to find (must occur exactly once)."),
				"new_string": strProp("Replacement text."),
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				if err := editLocalFile(ws, restrict, argStr(args, "path"), argStr(args, "old_string"), argStr(args, "new_string")); err != nil {
					return map[string]any{"success": false, "error": err.Error()}, nil
				}
				return map[string]any{"success": true, "path": argStr(args, "path")}, nil
			}),
		"exec": mk("exec", "Run a shell command. Default timeout 60s, max 300s; timed-out process group is killed.",
			objSchema([]string{"command"}, map[string]any{
				"command":         strProp("Shell command, e.g. \"go test ./...\" or \"ls -la\"."),
				"workdir":         strProp("Working dir inside workspace (default: workspace root)."),
				"timeout_seconds": map[string]any{"type": "number", "description": fmt.Sprintf("Timeout 1-%d detik (default %d).", maxExecTimeoutSec, defaultExecTimeoutSec)},
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				dir, err := resolveWorkdir(ws, restrict, argStr(args, "workdir"))
				if err != nil {
					return map[string]any{"success": false, "error": err.Error()}, nil
				}
				res := runExec(dir, argStr(args, "command"), clampTimeout(args["timeout_seconds"]))
				out := map[string]any{"success": res.Success, "exit_code": res.ExitCode, "output": res.Output}
				if res.TimedOut {
					out["timed_out"] = true
				}
				return out, nil
			}),
	}
}

func argStr(a map[string]any, k string) string {
	s, _ := a[k].(string)
	return s
}

func objSchema(required []string, props map[string]any) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
