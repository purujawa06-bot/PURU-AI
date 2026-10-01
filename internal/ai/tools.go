package ai

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// TelegramUser is the chat user asking (Telegram only; nil in CLI).
type TelegramUser struct {
	ID        int64
	Username  string
	FirstName string
	LastName  string
}

// TelegramUserInfo is full info about a Telegram user: name, id, username,
// plus bio when fetched live via the API.
type TelegramUserInfo struct {
	ID        int64
	Username  string
	FirstName string
	LastName  string
	Bio       string
}

// TelegramClient talks to Telegram. *telegram.API satisfies it;
// nil outside Telegram (CLI) so telegram_* tools report unavailability.
type TelegramClient interface {
	SendFile(ctx context.Context, chatID int64, filename string, data []byte, caption string) error
	GetTelegramUser(ctx context.Context, userID int64) (*TelegramUserInfo, error)
}

// maxSendFileBytes caps telegram_sendfile uploads (Telegram bots allow 50MB).
const maxSendFileBytes = 20 << 20

// BuildTools returns 14 tools by default: file tools (read_file, write_file,
// list_dir, edit_file, append_file) + exec + Telegram tools
// (telegram_sendfile, telegram_getuser) + get_env + web_fetch + schedule
// + spawn_agent + skill tools (use_skill, stop_skill). web_search
// (third-party, opt-in: Google AI Studio with googleSearch grounding and/or
// Exa) is added as the 15th tool only when at least one web_search provider
// is ready (active + credentials) in config.json. No PuruBoy API anywhere.
// opts carries workspace config, current chat/user, and the OnTool preview hook.
func BuildTools(a *Agent, opts *ProcessOptions) map[string]*Tool {
	ws := ""
	restrict := true
	if a != nil && a.Config != nil {
		ws = a.Config.Workspace
		restrict = a.Config.RestrictWorkspace
	}
	mk := func(name, desc string, params map[string]any, run func(ctx context.Context, args map[string]any) (any, error)) *Tool {
		return &Tool{Name: name, Description: desc, Parameters: params, Run: func(ctx context.Context, args map[string]any) (any, error) {
			if opts != nil && opts.OnTool != nil {
				opts.OnTool(name, args)
			}
			return run(ctx, args)
		}}
	}
	errVal := func(err error) (any, error) {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	tools := map[string]*Tool{
			"read_file": mk("read_file", "Read a file from the workspace. Use when you need file contents before answering, editing, or summarizing. Do NOT use to list directories — use list_dir. Returns the file text (truncated at max bytes), or {success:false,error} when missing or blocked. Read-only, no writes.",
				objSchema([]string{"path"}, map[string]any{
					"path":   strProp("Workspace-relative file path to read. Example: notes.txt, sub/a.txt."),
					"offset": intRangeProp("Byte offset to start reading from. Default 0.", 0, 0, 1073741824),
					"length": intRangeProp("Maximum number of bytes to read. Default 65536, max 65536.", maxReadFileSize, 1, 65536),
				}),
			func(ctx context.Context, args map[string]any) (any, error) {
				// Active skills stay readable: use_skill remains the
				// preferred loader, read_file is kept for debugging.
				length := int64(maxReadFileSize)
				if _, ok := args["length"]; ok {
					length = argInt(args, "length")
				}
				text, err := readLocalFile(ws, restrict, argStr(args, "path"), argInt(args, "offset"), length)
				if err != nil {
					return errVal(err)
				}
				return text, nil
			}),
		"write_file": mk("write_file", "Create a new file with the exact content given. Use when you need a file that does not exist yet. Do NOT use to change part of an existing file — use edit_file; to add at the end — use append_file. Returns \"File written: <path>\" on success, or {success:false,error} when the path is blocked or already exists. Writes the file: confirm with the user before replacing anything important, and prefer overwrite=false so accidental data loss fails loudly.",
			objSchema([]string{"path", "content"}, map[string]any{
				"path":      strProp("Workspace-relative file path to create, parent dirs are created. Example: notes.txt, skills/<name>/SKILL.md."),
				"content":   strProp("Full file content to write, verbatim. Empty string creates an empty file."),
				"overwrite": boolProp("Replace an existing file in full. Default false, so the call fails instead of discarding data. Example: {\"path\":\"notes.txt\",\"content\":\"...\",\"overwrite\":true}.", false),
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				content, ok := args["content"].(string)
				if !ok {
					return errVal(fmt.Errorf("content is required"))
				}
				if err := writeLocalFile(ws, restrict, argStr(args, "path"), content, argBool(args, "overwrite")); err != nil {
					return errVal(err)
				}
				return fmt.Sprintf("File written: %s", argStr(args, "path")), nil
			}),
		"list_dir": mk("list_dir", "List files and directories at one path. Use when you need to discover what exists before reading or editing. Do NOT use to read file contents — use read_file. Returns a plain-text listing (names one per line, directories marked), or {success:false,error} when the path is missing or blocked. Read-only, no writes.",
			objSchema([]string{"path"}, map[string]any{
				"path": strProp("Workspace-relative directory to list. Example: \".\" for the workspace root, or skills/find-skills."),
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				text, err := listLocalDir(ws, restrict, argStr(args, "path"))
				if err != nil {
					return errVal(err)
				}
				return text, nil
			}),
		"edit_file": mk("edit_file", "Replace one unique text block inside an existing file. Use for targeted changes to a file you have already read. Do NOT use to create a file — use write_file; to append — use append_file; for wholesale replacement — write_file with overwrite=true. Returns \"File edited: <path>\", or {success:false,error} when old_string is missing or ambiguous. Writes the file: re-read before editing and confirm destructive edits with the user.",
			objSchema([]string{"path", "old_string", "new_string"}, map[string]any{
				"path":       strProp("Workspace-relative path of the existing file to edit. Example: AGENTS.md."),
				"old_string": strProp("Exact text to find. Must occur exactly once in the file; if it occurs more than once the call fails and you must include more surrounding context."),
				"new_string": strProp("Replacement text. Use an empty string to delete the matched block."),
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				oldText, ok := args["old_string"].(string)
				if !ok || strings.TrimSpace(oldText) == "" {
					return errVal(fmt.Errorf("old_string is required"))
				}
				newText, ok := args["new_string"].(string)
				if !ok {
					return errVal(fmt.Errorf("new_string is required"))
				}
				if err := editLocalFile(ws, restrict, argStr(args, "path"), oldText, newText); err != nil {
					return errVal(err)
				}
				return fmt.Sprintf("File edited: %s", argStr(args, "path")), nil
			}),
		"append_file": mk("append_file", "Append content to the end of a file, creating it when missing. Use for logs, notes, and adding to a list. Do NOT use to change existing text — use edit_file; to create with exact content — write_file. Returns \"Appended to <path>\", or {success:false,error} when the path is blocked. Writes the file: the previous content is preserved, but confirm before appending to files the user owns.",
			objSchema([]string{"path", "content"}, map[string]any{
				"path":    strProp("Workspace-relative file path to append to. Example: memory/context-notes.md."),
				"content": strProp("Text to add after the current end of file. Include leading newlines when needed."),
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				content, ok := args["content"].(string)
				if !ok {
					return errVal(fmt.Errorf("content is required"))
				}
				if err := appendLocalFile(ws, restrict, argStr(args, "path"), content); err != nil {
					return errVal(err)
				}
				return fmt.Sprintf("Appended to %s", argStr(args, "path")), nil
			}),
			"exec": mk("exec", "Run a shell command inside the workspace and return its result. Use when you need to build, test, inspect git, or run any command the file tools cannot do. Do NOT use to read or edit files — use read_file/edit_file; to fetch URLs — use web_fetch. Returns {success, exit_code, output} on run, or {success:false,error} when blocked. Notes: writes and executes arbitrary commands inside the workspace jail (outside paths are rejected); destructive commands are irreversible — confirm with the user first. Long commands should run with background=true then poll/read; kill stops them. Timeouts are clamped 1-300s, output is capped.",
				objSchema([]string{"action"}, map[string]any{
					"action":     enumProp("Action: run (execute), list (show sessions), poll (check status), read (get output), kill (terminate)", []string{"run", "list", "poll", "read", "kill"}),
					"command":    strProp("Shell command to run, required for action=run. Example: go test ./... ."),
					"sessionId":  strProp("Session ID from a background run, required for poll/read/kill. Example: exec-1."),
					"background": boolProp("Run in background immediately and return a sessionId for poll/read. Default false.", false),
					"cwd":        strProp("Working directory inside the workspace. Default is the workspace root. Example: internal/ai."),
					"timeout":    intRangeProp("Timeout in seconds for action=run. Default 60, clamped 1-300.", defaultExecTimeoutSec, 1, 300),
				}),
			func(ctx context.Context, args map[string]any) (any, error) {
				action := argStr(args, "action")
				switch action {
				case "list":
					return listSessions(), nil
				case "poll":
					sid := argStr(args, "sessionId")
					if sid == "" {
						return errVal(fmt.Errorf("sessionId is required for poll"))
					}
					res, err := pollExec(sid)
					if err != nil {
						return errVal(err)
					}
					return res, nil
				case "read":
					sid := argStr(args, "sessionId")
					if sid == "" {
						return errVal(fmt.Errorf("sessionId is required for read"))
					}
					res, err := readExec(sid)
					if err != nil {
						return errVal(err)
					}
					return res, nil
				case "kill":
					sid := argStr(args, "sessionId")
					if sid == "" {
						return errVal(fmt.Errorf("sessionId is required for kill"))
					}
					res, err := killExec(sid)
					if err != nil {
						return errVal(err)
					}
					return res, nil
				case "run":
					command := argStr(args, "command")
					if command == "" {
						return errVal(fmt.Errorf("command is required for action \"run\""))
					}
					dir, err := resolveWorkdir(ws, restrict, argStr(args, "cwd"))
					if err != nil {
						return errVal(err)
					}
					timeout := int(argInt(args, "timeout"))
					memMB := defaultExecMemMB
					if a != nil && a.Config != nil {
						memMB = clampMemMB(a.Config.ExecMemoryMB)
					}
					return runExec(ctx, dir, command, clampTimeout(timeout), memMB, argBool(args, "background"))
				default:
					return errVal(fmt.Errorf("unsupported action: %s", action))
				}
			}),
		"telegram_sendfile": mk("telegram_sendfile", "Upload a workspace file to the current Telegram chat. Use when the user asks to receive a file, image, or generated artifact. Do NOT use to paste file contents in a message — use read_file and reply with text. Returns {success:true,path} on upload, or {error} when unavailable outside Telegram, too large, or blocked. Notes: sends a message to the user immediately and cannot be unsent — send only when the user asked for it. Max 20MB; filename defaults to the path basename.",
			objSchema([]string{"path"}, map[string]any{
				"path":     strProp("Workspace-relative path of the file to upload. Relative paths resolve from the workspace root. Example: out/report.pdf."),
				"filename": strProp("Display filename shown in Telegram. Default is the basename of path."),
				"caption":  strProp("Optional short caption shown under the file."),
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				if a == nil || a.Telegram == nil || opts == nil || opts.ChatID == 0 {
					return map[string]any{"error": "telegram_sendfile is only available in Telegram chat"}, nil
				}
				abs, err := resolvePath(ws, restrict, argStr(args, "path"))
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				stat, err := os.Stat(abs)
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				if stat.IsDir() {
					return map[string]any{"error": "path is a directory, not a file"}, nil
				}
				if stat.Size() > maxSendFileBytes {
					return map[string]any{"error": fmt.Sprintf("file too large: %s (max 20MB)", formatSize(stat.Size()))}, nil
				}
				b, err := os.ReadFile(abs)
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				name := argStr(args, "filename")
				if name == "" {
					name = filepath.Base(abs)
				}
				if err := a.Telegram.SendFile(ctx, opts.ChatID, name, b, argStr(args, "caption")); err != nil {
					return map[string]any{"success": false, "error": err.Error()}, nil
				}
				return map[string]any{"success": true, "path": argStr(args, "path")}, nil
			}),
		"telegram_getuser": mk("telegram_getuser", "Look up a Telegram user's id, username, and name. Use when you need to confirm who is talking or resolve a username. Do NOT use to store profile data in memory — only lasting facts belong in MEMORY.md. Returns {id, username, first_name, last_name, bio} (bio only when fetched for another user), or {error} outside Telegram or for an unknown id. Read-only.",
			objSchema(nil, map[string]any{
				"user_id": map[string]any{"type": "number", "minimum": 1, "description": "Telegram user id to look up. Omit or 0 to return the current sender from the prompt context."},
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				if uid := argInt(args, "user_id"); uid != 0 {
					if a == nil || a.Telegram == nil {
						return map[string]any{"error": "telegram_getuser is only available in Telegram chat"}, nil
					}
					info, err := a.Telegram.GetTelegramUser(ctx, uid)
					if err != nil {
						return map[string]any{"error": err.Error()}, nil
					}
					return userInfoMap(info), nil
				}
				if opts == nil || opts.User == nil {
					return map[string]any{"error": "telegram_getuser is only available in Telegram chat"}, nil
				}
				u := opts.User
				return userInfoMap(&TelegramUserInfo{ID: u.ID, Username: u.Username, FirstName: u.FirstName, LastName: u.LastName}), nil
			}),
		"get_env": mk("get_env", "Get runtime facts about this agent: OS, architecture, Go version, workspace path, jail mode, and memory use. Use when you need to check the platform before choosing commands. Do NOT use to read environment variables or secrets — this returns no secret values. Returns {os, arch, go_ver, workspace, restricted, memory_mb} with no parameters. Read-only.",
			objSchema(nil, nil),
			func(ctx context.Context, args map[string]any) (any, error) {
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				return map[string]any{
					"os":         runtime.GOOS,
					"arch":       runtime.GOARCH,
					"go_ver":     runtime.Version(),
					"workspace":  ws,
					"restricted": restrict,
					"memory_mb":  m.Alloc / 1024 / 1024,
				}, nil
			}),
		"web_fetch": mk("web_fetch", "Fetch one public http/https URL and return its content as text. Use when you already know the exact page URL, including following up a web_search result. Do NOT use to discover pages — use web_search; to read local files — use read_file. Returns the page text (default 5000 chars, clamped 1000-20000) or raw HTML with section=html; long pages tell you to call again with a larger offset. Returns {success:false,error} for private/local hosts, non-http schemes, or timeouts (120s). Read-only; sends no data out except the GET request.",
			objSchema([]string{"url"}, map[string]any{
				"url":     strProp("Public http/https URL to fetch. Local, private, and file:// URLs are rejected. Example: https://example.com/docs."),
				"section": enumProp("Content section: text (default, HTML stripped) or html (raw).", []string{"text", "html"}),
				"offset":  intRangeProp("Char offset to start reading from, for paginating long pages. Default 0.", 0, 0, 100000000),
				"length":  intRangeProp("Max output chars. Default 5000, clamped 1000-20000.", defaultFetchLength, 1000, 20000),
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				text, err := runWebFetch(ctx, argStr(args, "url"), argStr(args, "section"), int(argInt(args, "offset")), int(argInt(args, "length")))
				if err != nil {
					return errVal(err)
				}
				return text, nil
			}),
	}
	// web_search is opt-in only: registered when at least one web_search
	// provider is ready. Default builds exclude it entirely.
	if a != nil && a.Config != nil && a.Config.WebSearchEnabled() {
		searchCfg := a.Config.WebSearch
		tools["web_search"] = mk("web_search", "Search the web and return ranked result snippets. Use when you need current facts, news, docs, or anything not already known or in the workspace. Do NOT use to read a page you already have the URL for — use web_fetch. Returns text results (title, url, snippet); pass count for more or fewer (default 5, max 10). Returns {success:false,error} on empty query or provider failure (60s). Read-only, costs a third-party search API call.",
			objSchema([]string{"query"}, map[string]any{
				"query": strProp("Search query, non-empty. Example: 'golang slog handler test'."),
				"count": intRangeProp("Number of results to return. Default 5, clamped 1-10.", defaultSearchN, 1, 10),
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				q := argStr(args, "query")
				if err := validateSearchQuery(q); err != nil {
					return errVal(err)
				}
				n := clampSearchCount(argInt(args, "count"))
				text, err := runWebSearch(ctx, searchCfg, q, n)
				if err != nil {
					return errVal(err)
				}
				return text, nil
			})
	}
	tools["schedule"] = buildScheduleTool(a, opts, mk, errVal)
	tools["spawn_agent"] = buildSpawnTool(a, opts, mk, errVal)
	for name, tool := range buildSkillTools(a, opts, mk, errVal) {
		tools[name] = tool
	}
	return tools
}

func userInfoMap(u *TelegramUserInfo) map[string]any {
	return map[string]any{
		"id": u.ID, "username": u.Username,
		"first_name": u.FirstName, "last_name": u.LastName, "bio": u.Bio,
	}
}

func argInt(a map[string]any, k string) int64 {
	switch n := a[k].(type) {
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	case string:
		if v, err := strconv.ParseInt(n, 10, 64); err == nil {
			return v
		}
	}
	return 0
}

func argBool(a map[string]any, k string) bool {
	switch v := a[k].(type) {
	case bool:
		return v
	case string:
		return v == "true"
	}
	return false
}

func argStr(a map[string]any, k string) string {
	s, _ := a[k].(string)
	return strings.TrimSpace(s)
}

func objSchema(required []string, props map[string]any) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func intRangeProp(desc string, def, min, max int64) map[string]any {
	return map[string]any{"type": "integer", "description": desc, "default": def, "minimum": min, "maximum": max}
}

func intProp(desc string, def int64) map[string]any {
	return map[string]any{"type": "integer", "description": desc, "default": def}
}

func boolProp(desc string, def bool) map[string]any {
	return map[string]any{"type": "boolean", "description": desc, "default": def}
}

func enumProp(desc string, values []string) map[string]any {
	return map[string]any{"type": "string", "enum": values, "description": desc}
}
