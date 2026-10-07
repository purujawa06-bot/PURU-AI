package ai

import "fmt"

// allParamsDesc is the single source of truth for every parameter
// description. Each param name is defined once here and reused by all
// tools — path is path everywhere, start_line is start_line everywhere.
var allParamsDesc = map[string]string{
	"path":          "Absolute path on the host filesystem. Relative paths are rejected. When the workspace jail is active, the path must still live inside the workspace.",
	"start_line":    "1-based line number to start from. Default 1.",
	"length":        "Maximum number of lines to return.",
	"content":       "Text content to write or insert.",
	"overwrite":     "Replace the whole file when true. Default false; existing files error unless overwrite is set.",
	"old_string":    "Exact text to find. Must appear exactly once in the file.",
	"new_string":    "Text to replace old_string with.",
	"end_line":      "1-based last line to replace. Defaults to start_line (single line).",
	"action":        "The operation to perform; allowed values are listed in the enum.",
	"command":       "Shell command to execute. Required when action is \"run\".",
	"sessionId":     "Background session ID from a previous run with background=true. Required for action poll/read/kill.",
	"background":    "Run the command in the background and return a session ID instead of blocking. Default false.",
	"cwd":           "Absolute working directory for the command. Default: the configured workspace.",
	"timeout":       "Timeout in seconds. Default 60, max 300.",
	"filename":      "Display filename sent to Telegram. Optional; derived from path when omitted.",
	"caption":       "Optional caption shown under the file in Telegram.",
	"user_id":       "Telegram user ID. Omit to target the current chat.",
	"url":           "Public http/https URL to fetch. Not a local file path.",
	"section":       "Response format: \"text\" (default, readable text) or \"html\" (raw markup).",
	"query":         "Search query. Must be meaningful and non-empty.",
	"count":         "Number of results to return. Default 5, max 10.",
	"name":          "Name of the job or skill, as listed by the respective list action/catalog.",
	"prompt":        "Instruction sent to the agent when the job fires.",
	"type":          "Schedule type: once, every, daily, or cron.",
	"timezone":      "IANA timezone name (e.g. Asia/Jakarta). Default: host local timezone.",
	"run_once_at":   "When a \"once\" job runs. Accepts RFC3339, \"YYYY-MM-DD HH:MM\", \"YYYY-MM-DD\", or \"HH:MM\" (today, or tomorrow if already past).",
	"every_seconds": "Interval in seconds for \"every\" jobs. Default 3600, minimum 60.",
	"daily_time":    "Time of day in HH:MM (24-hour), interpreted in the job's timezone.",
	"weekdays":      "Days of week the job runs: mon, tue, wed, thu, fri, sat, sun.",
	"cron_expr":     "5-field cron expression: minute hour day-of-month month day-of-week.",
	"end_at":        "Stop time after which the job no longer fires. Same flexible formats as run_once_at.",
	"days":          "How many days the job stays active. Default 0 means no end.",
	"max_runs":      "Maximum number of runs before the job is removed. Default 0 means unlimited.",
	"job_id":        "ID of the scheduled job, as returned by action \"list\" or \"add\".",
	"always_active": "Keep the skill active across turns until explicitly stopped. Default false.",
}

// allToolsDesc is the single source of truth for every tool description.
var allToolsDesc = map[string]string{
	"read_file":         "Read a text file from the local workspace and return its contents with line numbers, optionally sliced by start_line/length. Read a file before editing it and reuse the content already in context instead of re-reading. Default limit is 200 lines.",
	"write_file":        "Write text to a file, creating parent directories as needed. Fails if the file exists unless overwrite is true. Use edit_file for targeted changes to existing files.",
	"list_dir":          "List the entries of a directory (files and subfolders). Path follows the same absolute-path rules as read_file. Prefer this over run_shell_command ls for browsing the workspace.",
	"edit_file":         "Replace an exact occurrence of old_string with new_string in a file. old_string must match exactly once; include surrounding context to make it unique. Use edit_file_by_line when you only know line numbers.",
	"edit_file_by_line": "Replace the line range start_line..end_line with new content. Lines are 1-based. Prefer edit_file when you can match exact text.",
	"append_file":       "Append text to the end of a file, creating it if missing. Use for logs and incremental output; for edits inside the file use edit_file.",
	"run_shell_command": "Run or manage shell commands on the host. Use action=run to execute (set background=true for long commands, then poll with action=poll and the returned sessionId). action=list shows sessions, action=read reads output, action=kill stops a running session. Commands run with cwd inside the workspace unless overridden.",
	"telegram_sendfile": "Send a local file to the Telegram chat as a document. Use filename to override the display name and caption for an accompanying message.",
	"telegram_getuser":  "Fetch Telegram profile information for a user. Omit user_id to look up the current chat.",
	"get_env":           "Return runtime environment information: OS, architecture, Go version, workspace path, workspace-restriction flag, and Go memory usage (MB). Use to diagnose the environment instead of shelling out.",
	"web_fetch":         "Fetch a public http/https URL and return its content as text or HTML, optionally sliced by start_line/length. For discovery use web_search first; this fetches one known URL.",
	"web_search":        "Search the web and return ranked results. Use to find URLs or current information, then call web_fetch on the most relevant link. Requires web search to be configured (opt-in).",
	"manage_schedule":   "Create, list, update, remove, and enable/disable scheduled agent jobs. A job runs its prompt on its schedule (once, every N seconds, daily at a time, or cron). Use job_id from action=list/add for get/update/remove/enable/disable. Maximum duration and run limits are set via days/max_runs; both default to no limit.",
	"use_skill":         "Activate a skill by name, loading its instructions into the conversation. Use this before performing a task covered by an installed skill. Set always_active to keep it active across turns.",
	"stop_skill":        "Deactivate a previously activated skill by name, removing its instructions from the conversation.",
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
