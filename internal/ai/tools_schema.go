package ai

import "fmt"

// allParamsDesc is the single source of truth for every parameter
// description. Each param name is defined once here and reused by all
// tools — path is path everywhere, start_line is start_line everywhere.
var allParamsDesc = map[string]string{
	"path":          "Absolute path.",
	"start_line":    "Start line; 1-based.",
	"length":        "Max lines.",
	"content":       "Content to write.",
	"overwrite":     "Replace whole file.",
	"old_string":    "Text to find; must be unique.",
	"new_string":    "Replacement text.",
	"end_line":      "Last line; defaults to start_line.",
	"action":        "Action to run.",
	"command":       "Shell command.",
	"sessionId":     "Background session ID.",
	"background":    "Run in background.",
	"cwd":           "Absolute working dir; default workspace.",
	"timeout":       "Timeout seconds.",
	"filename":      "Display filename.",
	"caption":       "File caption.",
	"user_id":       "Telegram user ID.",
	"url":           "Public http/https URL.",
	"section":       "text or html.",
	"query":         "Search query.",
	"count":         "Result count.",
	"name":          "Name.",
	"prompt":        "Instruction to run.",
	"type":          "Schedule type: once, every, daily, cron.",
	"timezone":      "IANA timezone.",
	"run_once_at":   "When a once job runs.",
	"every_seconds": "Interval seconds.",
	"daily_time":    "Time of day HH:MM.",
	"weekdays":      "Days to run.",
	"cron_expr":     "5-field cron.",
	"end_at":        "Stop time.",
	"days":          "Active days; 0 = no end.",
	"max_runs":      "Max runs; 0 = unlimited.",
	"job_id":        "Job ID.",
	"always_active": "Keep active across turns.",
}

// allToolsDesc is the single source of truth for every tool description.
var allToolsDesc = map[string]string{
	"read_file":         "Read text file.",
	"write_file":        "Write file.",
	"list_dir":          "List folder.",
	"edit_file":         "Replace text in file.",
	"edit_file_by_line": "Replace lines in file.",
	"append_file":       "Append to file.",
	"run_shell_command": "Run shell.",
	"telegram_sendfile": "Send file to Telegram chat.",
	"telegram_getuser":  "Telegram user info.",
	"get_env":           "Runtime env.",
	"web_fetch":         "Fetch URL as text.",
	"web_search":        "Search web.",
	"manage_schedule":   "Manage scheduled jobs.",
	"use_skill":         "Activate skill.",
	"stop_skill":        "Deactivate skill.",
}

// toolSchemaEntry is one tool entry: description + params.
type toolSchemaEntry struct {
	Description string         `json:"description"`
	Required    []string       `json:"required"`
	Properties  map[string]any `json:"properties"`
}

func strParam(name string) map[string]any {
	return map[string]any{"type": "string", "description": allParamsDesc[name]}
}

// toolSchemas wires every tool to the global descriptions above.
// Per-tool differences (defaults, enums, required) stay here; the
// description strings themselves live only in allParamsDesc/allToolsDesc.
var toolSchemas = map[string]toolSchemaEntry{
	"read_file": {
		Description: allToolsDesc["read_file"],
		Required:    []string{"path"},
		Properties: map[string]any{
			"path":       strParam("path"),
			"start_line": map[string]any{"type": "integer", "description": allParamsDesc["start_line"], "default": 1, "minimum": 1, "maximum": 1000000},
			"length":     map[string]any{"type": "integer", "description": allParamsDesc["length"], "default": 200, "minimum": 1, "maximum": 2000},
		},
	},
	"write_file": {
		Description: allToolsDesc["write_file"],
		Required:    []string{"path", "content"},
		Properties: map[string]any{
			"path":      strParam("path"),
			"content":   strParam("content"),
			"overwrite": map[string]any{"type": "boolean", "description": allParamsDesc["overwrite"], "default": false},
		},
	},
	"list_dir": {
		Description: allToolsDesc["list_dir"],
		Required:    []string{"path"},
		Properties: map[string]any{
			"path": strParam("path"),
		},
	},
	"edit_file": {
		Description: allToolsDesc["edit_file"],
		Required:    []string{"path", "old_string", "new_string"},
		Properties: map[string]any{
			"path":       strParam("path"),
			"old_string": strParam("old_string"),
			"new_string": strParam("new_string"),
		},
	},
	"edit_file_by_line": {
		Description: allToolsDesc["edit_file_by_line"],
		Required:    []string{"path", "start_line", "content"},
		Properties: map[string]any{
			"path":       strParam("path"),
			"start_line": map[string]any{"type": "integer", "description": allParamsDesc["start_line"], "minimum": 1, "maximum": 1000000},
			"end_line":   map[string]any{"type": "integer", "description": allParamsDesc["end_line"], "minimum": 1, "maximum": 1000000},
			"content":    strParam("content"),
		},
	},
	"append_file": {
		Description: allToolsDesc["append_file"],
		Required:    []string{"path", "content"},
		Properties: map[string]any{
			"path":    strParam("path"),
			"content": strParam("content"),
		},
	},
	"run_shell_command": {
		Description: allToolsDesc["run_shell_command"],
		Required:    []string{"action"},
		Properties: map[string]any{
			"action":     map[string]any{"type": "string", "description": allParamsDesc["action"], "enum": []string{"run", "list", "poll", "read", "kill"}},
			"command":    strParam("command"),
			"sessionId":  strParam("sessionId"),
			"background": map[string]any{"type": "boolean", "description": allParamsDesc["background"], "default": false},
			"cwd":        strParam("cwd"),
			"timeout":    map[string]any{"type": "integer", "description": allParamsDesc["timeout"], "default": 60, "minimum": 1, "maximum": 300},
		},
	},
	"telegram_sendfile": {
		Description: allToolsDesc["telegram_sendfile"],
		Required:    []string{"path"},
		Properties: map[string]any{
			"path":     strParam("path"),
			"filename": strParam("filename"),
			"caption":  strParam("caption"),
		},
	},
	"telegram_getuser": {
		Description: allToolsDesc["telegram_getuser"],
		Properties: map[string]any{
			"user_id": map[string]any{"type": "integer", "description": allParamsDesc["user_id"], "minimum": 1},
		},
	},
	"get_env": {
		Description: allToolsDesc["get_env"],
		Properties:  map[string]any{},
	},
	"web_fetch": {
		Description: allToolsDesc["web_fetch"],
		Required:    []string{"url"},
		Properties: map[string]any{
			"url":        strParam("url"),
			"section":    map[string]any{"type": "string", "description": allParamsDesc["section"], "enum": []string{"text", "html"}},
			"start_line": map[string]any{"type": "integer", "description": allParamsDesc["start_line"], "default": 1, "minimum": 1, "maximum": 1000000},
			"length":     map[string]any{"type": "integer", "description": allParamsDesc["length"], "default": 100, "minimum": 1, "maximum": 1000},
		},
	},
	"web_search": {
		Description: allToolsDesc["web_search"],
		Required:    []string{"query"},
		Properties: map[string]any{
			"query": strParam("query"),
			"count": map[string]any{"type": "integer", "description": allParamsDesc["count"], "default": 5, "minimum": 1, "maximum": 10},
		},
	},
	"manage_schedule": {
		Description: allToolsDesc["manage_schedule"],
		Required:    []string{"action"},
		Properties: map[string]any{
			"action":        map[string]any{"type": "string", "description": allParamsDesc["action"], "enum": []string{"add", "list", "get", "update", "remove", "enable", "disable"}},
			"name":          strParam("name"),
			"prompt":        strParam("prompt"),
			"type":          map[string]any{"type": "string", "description": allParamsDesc["type"], "enum": []string{"once", "every", "daily", "cron"}},
			"timezone":      strParam("timezone"),
			"run_once_at":   strParam("run_once_at"),
			"every_seconds": map[string]any{"type": "integer", "description": allParamsDesc["every_seconds"], "default": 3600, "minimum": 60, "maximum": 31622400},
			"daily_time":    strParam("daily_time"),
			"weekdays":      map[string]any{"type": "array", "description": allParamsDesc["weekdays"], "items": map[string]any{"type": "string", "enum": []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}}},
			"cron_expr":     strParam("cron_expr"),
			"end_at":        strParam("end_at"),
			"days":          map[string]any{"type": "integer", "description": allParamsDesc["days"], "default": 0, "minimum": 0, "maximum": 366},
			"max_runs":      map[string]any{"type": "integer", "description": allParamsDesc["max_runs"], "default": 0, "minimum": 0, "maximum": 10000},
			"job_id":        strParam("job_id"),
		},
	},
	"use_skill": {
		Description: allToolsDesc["use_skill"],
		Required:    []string{"name"},
		Properties: map[string]any{
			"name":          strParam("name"),
			"always_active": map[string]any{"type": "boolean", "description": allParamsDesc["always_active"], "default": false},
		},
	},
	"stop_skill": {
		Description: allToolsDesc["stop_skill"],
		Required:    []string{"name"},
		Properties: map[string]any{
			"name": strParam("name"),
		},
	},
}

// loadToolSchemas returns the global schemas.
func loadToolSchemas() (map[string]toolSchemaEntry, error) {
	if len(toolSchemas) == 0 {
		return nil, fmt.Errorf("toolSchemas empty")
	}
	return toolSchemas, nil
}

// mustEntry fetches one tool entry and panics when it is missing or has an
// empty description, so a typo fails at build time (go test) instead of
// silently shipping a tool the model cannot use.
func mustEntry(name string) toolSchemaEntry {
	e, ok := toolSchemas[name]
	if !ok {
		panic(fmt.Sprintf("tool %q missing in toolSchemas", name))
	}
	if e.Description == "" {
		panic(fmt.Sprintf("tool %q has empty description", name))
	}
	return e
}

// toolDescription returns the description for name from allToolsDesc.
func toolDescription(name string) string {
	return mustEntry(name).Description
}

// toolParameters returns a fresh params object for name:
// {type:"object", properties:{...}, required:[...] when non-empty}.
// Copied per call so each BuildTools gets its own map — the agent layer
// mutates these (sanitizeParams) when building function definitions.
func toolParameters(name string) map[string]any {
	e := mustEntry(name)
	props := make(map[string]any, len(e.Properties))
	for k, v := range e.Properties {
		props[k] = v
	}
	out := map[string]any{"type": "object", "properties": props}
	if len(e.Required) > 0 {
		req := make([]string, len(e.Required))
		copy(req, e.Required)
		out["required"] = req
	}
	return out
}
