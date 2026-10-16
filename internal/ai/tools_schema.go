package ai

import "fmt"

// allParamsDesc is the single source of truth for every parameter
// description. Each param name is defined once here and reused by all
// tools — path is path everywhere, start_line is start_line everywhere.
var allParamsDesc = map[string]string{
	"path":          "Absolute path to the workspace. If get_env restricted is true, it may only point to the workspace and must not point outside it.",
	"start_line":    "1-based line number to start from. Default 1.",
	"length":        "Maximum number of lines to return.",
	"content":       "Text content to write or insert.",
	"overwrite":     "Replace the whole file when true. Default false; existing files error unless overwrite is set.",
	"old_string":    "Exact text to find; must match exactly once. Use edit_file_by_line to edit by line numbers instead.",
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

// allOutputs is the single source of truth for every message a tool returns:
// success text, status values, and the error text the model reads. Edit the
// wording here; tool code only references the key through outputText/outputf/
// outputErr, so no message has to be hunted across files.
//
// Keys are "<tool-or-helper>.<case>" and templates use fmt verbs (e.g. %s, %q,
// %d, %w). Layout glue (list_dir "DIR:"/"FILE:" lines, read_file "N|content"
// line numbers, web result numbering) intentionally stays in the tool code:
// it is formatting, not a message.
var allOutputs = map[string]string{
	// Shared path/workspace helpers (fs.go), used by every file tool.
	"fs.path_empty":               "path is empty",
	"fs.path_not_absolute":        "path must be absolute: %q",
	"fs.outside_workspace":        "outside workspace: %q",
	"fs.workspace_not_found":      "workspace not found: %s",
	"fs.workspace_not_accessible": "workspace is not accessible: %s: %v",
	"fs.workspace_not_dir":        "workspace is not a directory: %s",
	"fs.cwd_not_found":            "cwd not found: %s. Use list_dir to see workspace contents",
	"fs.cwd_not_accessible":       "cwd is not accessible: %s: %v",
	"fs.cwd_not_dir":              "cwd is not a directory: %s. Use list_dir to see workspace contents",
	"fs.content_required":         "content is required",

	// read_file.
	"read_file.length_min":    "length must be > 0",
	"read_file.not_found":     "file not found: %s",
	"read_file.access_denied": "access denied to file: %s",
	"read_file.open_failed":   "failed to open file: %w",
	"read_file.stat_failed":   "failed to stat file: %w",
	"read_file.is_dir":        "path is a directory: %s. Use list_dir to see its contents",
	"read_file.binary":        "file appears to be binary",
	"read_file.read_failed":   "failed to read file content: %w",

	// write_file.
	"write_file.done":   "File written: %s",
	"write_file.is_dir": "cannot write: %s is a directory. Use list_dir to see its contents",
	"write_file.exists": "file %s already exists. Set overwrite=true to replace it, or use edit_file / edit_file_by_line for partial changes.",

	// list_dir.
	"list_dir.read_failed":  "failed to read directory: %w",
	"list_dir.empty":        "(empty directory)",
	"list_dir.unknown_size": "unknown size",
	"list_dir.truncated":    "\n... [truncated, %d more entries]",

	// edit_file / edit_file_by_line.
	"edit_file.done":                  "File edited: %s",
	"edit_file.read_failed":           "read %s: %w",
	"edit_file.old_required":          "old_string is required. Use edit_file_by_line to edit by line numbers instead.",
	"edit_file.new_required":          "new_string is required",
	"edit_file.old_empty":             "old_string is empty. Use edit_file_by_line to edit by line numbers instead.",
	"edit_file.old_ambiguous":         "old_string found %d times; add more context, or use edit_file_by_line.",
	"edit_file.fuzzy_ambiguous":       "fuzzy match found %d times; add more context, or use edit_file_by_line.",
	"edit_file.old_missing":           "old_string not found in %s; match the exact text (indentation included), or use edit_file_by_line.",
	"edit_file_by_line.start_min":     "start_line must be >= 1",
	"edit_file_by_line.end_first":     "end_line (%d) < start_line (%d)",
	"edit_file_by_line.out_of_bounds": "line range %d-%d out of bounds (file has %d lines)",

	// append_file.
	"append_file.done": "Appended to %s",

	// run_shell_command (tools.go + exec.go).
	"run_shell_command.command_required":        "command is required for action \"run\"",
	"run_shell_command.session_required":        "sessionId is required for %s",
	"run_shell_command.unsupported_action":      "unsupported action: %s",
	"run_shell_command.empty_command":           "command is empty",
	"run_shell_command.session_not_found":       "session not found: %s",
	"run_shell_command.no_sessions":             "No active sessions.",
	"run_shell_command.truncated":               "\n...[truncated, total %s]",
	"run_shell_command.status_running":          "running",
	"run_shell_command.status_finished":         "finished",
	"run_shell_command.status_killed":           "killed",
	"run_shell_command.status_already_finished": "already finished",

	// telegram_sendfile / telegram_getuser.
	"telegram_sendfile.unavailable": "telegram_sendfile is only available in Telegram chat",
	"telegram_sendfile.is_dir":      "path is a directory, not a file",
	"telegram_sendfile.too_large":   "file too large: %s (max 20MB)",
	"telegram_getuser.unavailable":  "telegram_getuser is only available in Telegram chat",

	// web_fetch.
	"web_fetch.url_required":       "url is required",
	"web_fetch.invalid_url":        "invalid url: %v",
	"web_fetch.scheme_rejected":    "url must be http/https (rejected %s)",
	"web_fetch.host_required":      "url must have a host",
	"web_fetch.private_rejected":   "local/private host rejected: %s",
	"web_fetch.redirect_rejected":  "redirect to private host rejected: %s",
	"web_fetch.too_many_redirects": "too many redirects",
	"web_fetch.http_error":         "fetch HTTP %d: %s",
	"web_fetch.page_empty":         "page is empty",
	"web_fetch.start_beyond":       "start_line %d beyond content lines %d",
	"web_fetch.more_pages":         " ... [truncated, total %d lines, start_line %d — call web_fetch again with section %s start_line %d length %d for more]",

	// web_search.
	"web_search.query_required":         "query is required",
	"web_search.disabled":               "web_search is disabled (enable web_search.aistudio or web_search.exa in config.json)",
	"web_search.aistudio_unconfigured":  "web_search aistudio model/api key is not configured",
	"web_search.exa_unconfigured":       "web_search exa api key is not configured",
	"web_search.aistudio_http":          "search API HTTP %d: %s",
	"web_search.aistudio_bad_json":      "search API bad JSON: %v",
	"web_search.aistudio_api_error":     "search API error",
	"web_search.aistudio_no_candidates": "search API returned no candidates",
	"web_search.no_results":             "search API returned no results (check connection/query)",
	"web_search.exa_http":               "exa search HTTP %d: %s",
	"web_search.exa_bad_json":           "exa search bad JSON: %v",
	"web_search.failed":                 "web search failed: %s",
	"web_search.aistudio_prefix":        "aistudio: %s",
	"web_search.exa_prefix":             "exa: %s",
	"web_search.answer_title":           "AI Studio answer",

	// manage_schedule.
	"manage_schedule.unavailable":        "manage_schedule unavailable: workspace not configured",
	"manage_schedule.unsupported_action": "unsupported action: %s",
	"manage_schedule.no_jobs":            "No scheduled jobs.",
	"manage_schedule.get":                "%s\nPrompt: %s",
	"manage_schedule.removed":            "Removed job %s.",
	"manage_schedule.toggled":            "Job %s %sd: %s.",
	"manage_schedule.scheduled":          "Scheduled: %s.",
	"manage_schedule.updated":            "Updated: %s.",
	"manage_schedule.job_id_required":    "job_id is required for %s",
	"manage_schedule.prompt_required":    "prompt is required (what should run)",
	"manage_schedule.type_required":      "type is required: once, every, daily, or cron",
	"manage_schedule.type_invalid":       "type must be once, every, daily, or cron",
	"manage_schedule.once_required":      "run_once_at is required for one-time jobs (e.g. 18:00)",
	"manage_schedule.once_switch":        "run_once_at is required when switching to once",
	"manage_schedule.daily_required":     "daily_time is required (HH:MM 24-hour, e.g. 06:00)",
	"manage_schedule.daily_switch":       "daily_time is required when switching to daily",
	"manage_schedule.cron_required":      "cron_expr is required (5 fields, e.g. 0 6 * * *)",
	"manage_schedule.cron_switch":        "cron_expr is required when switching to cron",
	"manage_schedule.days_negative":      "days cannot be negative",
	"manage_schedule.days_max":           "days must be under 366",
	"manage_schedule.max_runs_negative":  "max_runs cannot be negative",
	"manage_schedule.max_runs_max":       "max_runs must be under 10000",

	// use_skill / stop_skill.
	"use_skill.name_required":  "name is required (exact <name> from the <skills> catalog)",
	"use_skill.unavailable":    "use_skill unavailable: workspace not configured",
	"use_skill.unknown":        "unknown skill %q (check the <skills> catalog)",
	"use_skill.blocked":        "skill %q is blocked by skills policy",
	"use_skill.no_body":        "skill %q has no readable SKILL.md",
	"use_skill.empty_body":     "(No extended instructions in SKILL.md; name and description from the catalog apply.)",
	"use_skill.persist_failed": "persist active skill: %w",
	"use_skill.loaded_once":    "Skill %q loaded for this turn only (idle after final answer; pass always_active=true to persist).\n\n%s",
	"use_skill.loaded_no_chat": "Skill %q loaded for this turn only (no chat scope to persist).\n\n%s",
	"use_skill.loaded_plain":   "Skill %q loaded for this turn only.\n\n%s",
	"use_skill.already_active": "Skill %q is already active.\n\n%s",
	"use_skill.activated":      "Skill %q activated (always_active). Its full body is now injected as Active Skills and stays active until stop_skill.\n\n%s",
	"stop_skill.name_required": "name is required",
	"stop_skill.no_chat_scope": "stop_skill unavailable without chat scope",
	"stop_skill.unavailable":   "stop_skill unavailable: workspace not configured",
	"stop_skill.not_active":    "Skill %q is not runtime-active (frontmatter defaults, if any, stay active).",
	"stop_skill.deactivated":   "Skill %q deactivated. It returns to the <skills> catalog and is no longer injected.",
}

// outputText returns the message template for key. A missing or empty key
// panics, so a typo fails in go test instead of shipping empty tool output.
func outputText(key string) string {
	t, ok := allOutputs[key]
	if !ok || t == "" {
		panic(fmt.Sprintf("output key %q missing in allOutputs", key))
	}
	return t
}

// outputf renders the template for key with fmt arguments.
func outputf(key string, args ...any) string {
	return fmt.Sprintf(outputText(key), args...)
}

// outputErr builds an error from the template for key.
func outputErr(key string, args ...any) error {
	return fmt.Errorf(outputText(key), args...)
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
