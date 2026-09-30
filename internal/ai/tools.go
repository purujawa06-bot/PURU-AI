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

// BuildTools returns 15 tools by default: file tools (read_file, write_file,
// list_dir, edit_file_replace_string, edit_file_replace_line,
// edit_file_apply_patch, append_file) + exec + Telegram tools
// (telegram_sendfile, telegram_getuser) + get_env + web_fetch + schedule
// + skill tools (use_skill, stop_skill). web_search (third-party Google
// AI Studio with googleSearch grounding) is added as the 16th tool only
// when web_search.aistudio.active is true with model + api key set in
// config.json. No PuruBoy API anywhere.
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
		"read_file": mk("read_file", "Read a file. Reading skills/<name>/SKILL.md is allowed for debugging, including active skills.",
			objSchema([]string{"path"}, map[string]any{
				"path":   strProp("Path to the file to read."),
				"offset": intProp("Byte offset to start reading from.", 0),
				"length": intProp("Maximum number of bytes to read.", maxReadFileSize),
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
		"write_file": mk("write_file", "Write content to a file. Fails when the file already exists unless overwrite=true; use edit_file_* or append_file for partial changes.",
			objSchema([]string{"path", "content"}, map[string]any{
				"path":      strProp("Path to the file to write"),
				"content":   strProp("Content to write to the file."),
				"overwrite": boolProp("Set to true to replace an existing file in full, for example {\"path\": \"notes.txt\", \"content\": \"...\", \"overwrite\": true}. This discards the file's current contents.", false),
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
		"list_dir": mk("list_dir", "List files and directories.",
			objSchema([]string{"path"}, map[string]any{
				"path": strProp("Path to list"),
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				text, err := listLocalDir(ws, restrict, argStr(args, "path"))
				if err != nil {
					return errVal(err)
				}
				return text, nil
			}),
		"edit_file_replace_string": mk("edit_file_replace_string", "Replace text in a file.",
			objSchema([]string{"path", "old_text", "new_text"}, map[string]any{
				"path":     strProp("The file path to edit"),
				"old_text": strProp("The text to find and replace (must occur exactly once)."),
				"new_text": strProp("The text to replace with."),
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				oldText, ok := args["old_text"].(string)
				if !ok {
					return errVal(fmt.Errorf("old_text is required"))
				}
				newText, ok := args["new_text"].(string)
				if !ok {
					return errVal(fmt.Errorf("new_text is required"))
				}
				if err := editLocalFile(ws, restrict, argStr(args, "path"), oldText, newText); err != nil {
					return errVal(err)
				}
				return fmt.Sprintf("File edited: %s", argStr(args, "path")), nil
			}),
		"edit_file_replace_line": mk("edit_file_replace_line", "Replace lines in a file.",
			objSchema([]string{"path", "start_line", "new_text"}, map[string]any{
				"path":       strProp("The file path to edit"),
				"start_line": intProp("First line to replace (1-based).", 1),
				"end_line":   intProp("Last line to replace, inclusive (default: same as start_line).", 0),
				"new_text":   strProp("Replacement text, may span multiple lines. Empty string deletes the range."),
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				newText, ok := args["new_text"].(string)
				if !ok {
					return errVal(fmt.Errorf("new_text is required"))
				}
				start := argInt(args, "start_line")
				end := argInt(args, "end_line")
				if end <= 0 {
					end = start
				}
				if err := editLocalFileByLine(ws, restrict, argStr(args, "path"), start, end, newText); err != nil {
					return errVal(err)
				}
				return fmt.Sprintf("File edited: %s", argStr(args, "path")), nil
			}),
		"edit_file_apply_patch": mk("edit_file_apply_patch", "Apply a unified diff patch to a file.",
			objSchema([]string{"path", "patch"}, map[string]any{
				"path":  strProp("The file path to patch"),
				"patch": strProp("Unified diff text with one or more @@ hunks (file headers like '---'/'+++' are optional)."),
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				patch, ok := args["patch"].(string)
				if !ok || strings.TrimSpace(patch) == "" {
					return errVal(fmt.Errorf("patch is required"))
				}
				res, err := editLocalFileApplyPatch(ws, restrict, argStr(args, "path"), patch)
				if err != nil {
					return errVal(err)
				}
				return res, nil
			}),
		"append_file": mk("append_file", "Append content to a file.",
			objSchema([]string{"path", "content"}, map[string]any{
				"path":    strProp("The file path to append to"),
				"content": strProp("The content to append."),
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
		"exec": mk("exec", "Run shell commands.",
			objSchema([]string{"action"}, map[string]any{
				"action":     enumProp("Action: run (execute), list (show sessions), poll (check status), read (get output), kill (terminate)", []string{"run", "list", "poll", "read", "kill"}),
				"command":    strProp("Shell command (required for run)"),
				"sessionId":  strProp("Session ID (required for poll/read/kill)"),
				"background": boolProp("Run in background immediately (returns sessionId)", false),
				"cwd":        strProp("Working directory inside workspace."),
				"timeout":    intProp("Timeout in seconds (default 60, max 300).", defaultExecTimeoutSec),
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
		"telegram_sendfile": mk("telegram_sendfile", "Send a file to the current chat.",
			objSchema([]string{"path"}, map[string]any{
				"path":     strProp("Path to the local file. Relative paths are resolved from workspace."),
				"filename": strProp("Optional display filename. Defaults to the basename of path."),
				"caption":  strProp("Optional caption for the file."),
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
		"telegram_getuser": mk("telegram_getuser", "Get Telegram user info.",
			objSchema(nil, map[string]any{
				"user_id": map[string]any{"type": "number", "description": "Optional Telegram user id to look up (default: current requester)."},
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
		"get_env": mk("get_env", "Get environment info.",
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
		"web_fetch": mk("web_fetch", "Fetch a URL as text or HTML.",
			objSchema([]string{"url"}, map[string]any{
				"url":     strProp("Public http/https URL to fetch (required)."),
				"section": enumProp("Content section: text (default, stripped) or html (raw).", []string{"text", "html"}),
				"offset":  intProp("Char offset to start reading from (default 0).", 0),
				"length":  intProp("Max output chars (default 5000, max 20000).", defaultFetchLength),
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				text, err := runWebFetch(ctx, argStr(args, "url"), argStr(args, "section"), int(argInt(args, "offset")), int(argInt(args, "length")))
				if err != nil {
					return errVal(err)
				}
				return text, nil
			}),
	}
	// web_search is opt-in only: registered when web_search.aistudio is
	// active with model + api key. Default builds exclude it entirely.
	if a != nil && a.Config != nil && a.Config.WebSearchEnabled() {
		searchCfg := a.Config.WebSearch.AIStudio
		tools["web_search"] = mk("web_search", "Search the web via Google AI Studio (grounded).",
			objSchema([]string{"query"}, map[string]any{
				"query": strProp("Search query (required, non-empty)."),
				"count": intProp("Number of results (default 5, max 10).", defaultSearchN),
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

func intProp(desc string, def int64) map[string]any {
	return map[string]any{"type": "integer", "description": desc, "default": def}
}

func boolProp(desc string, def bool) map[string]any {
	return map[string]any{"type": "boolean", "description": desc, "default": def}
}

func enumProp(desc string, values []string) map[string]any {
	return map[string]any{"type": "string", "enum": values, "description": desc}
}
