package ai

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

// BuildTools returns 8 tools: 6 local workspace tools with picoclaw-mirrored
// declarations (read_file, write_file, list_dir, edit_file, append_file, exec)
// + 2 Telegram tools (telegram_sendfile, telegram_getuser, only usable with
// a Telegram context). opts carries workspace config, current chat/user, and
// the OnTool preview hook.
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
	return map[string]*Tool{
		"read_file": mk("read_file", "Read the contents of a file. Supports pagination via `offset` and `length`.",
			objSchema([]string{"path"}, map[string]any{
				"path":   strProp("Path to the file to read."),
				"offset": intProp("Byte offset to start reading from.", 0),
				"length": intProp("Maximum number of bytes to read.", maxReadFileSize),
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
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
		"write_file": mk("write_file", "Write content to a file, replacing any existing content. Content is written byte-for-byte after argument decoding. If the file already exists you must set overwrite=true, which replaces the ENTIRE file. To add to or change part of an existing file without losing its current contents, use append_file or edit_file instead.",
			objSchema([]string{"path", "content"}, map[string]any{
				"path":      strProp("Path to the file to write"),
				"content":   strProp("Content to write to the file."),
				"overwrite": boolProp("Set to true to replace an existing file in full. This discards the file's current contents.", false),
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
		"list_dir": mk("list_dir", "List files and directories in a path",
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
		"edit_file": mk("edit_file", "Edit a file by replacing old_text with new_text. The old_text must exist exactly in the file.",
			objSchema([]string{"path", "old_text", "new_text"}, map[string]any{
				"path":     strProp("The file path to edit"),
				"old_text": strProp("The exact text to find and replace."),
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
		"append_file": mk("append_file", "Append content to the end of a file.",
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
		"exec": mk("exec", "Execute shell commands. Action must be \"run\". Strictly capped: timeout 60s default (max 300s), RAM capped by exec_memory_mb (group killed when over), files capped 100MB, output truncated 20k chars.",
			objSchema([]string{"action"}, map[string]any{
				"action":     enumProp("Action: run (execute command), list (show sessions), poll (check status), read (get output), write (send input), kill (terminate), send-keys (send keys to PTY)", []string{"run", "list", "poll", "read", "write", "kill", "send-keys"}),
				"command":    strProp("Shell command to execute (required for run)"),
				"sessionId":  strProp("Session ID (required for poll/read/write/kill/send-keys)"),
				"keys":       strProp("Key names for send-keys: up, down, left, right, enter, tab, escape, backspace, ctrl-c, ctrl-d, home, end, pageup, pagedown, f1-f12"),
				"data":       strProp("Data to write to stdin (required for write)"),
				"background": strProp("Run in background immediately"),
				"pty":        strProp("Run in a pseudo-terminal (PTY) when available"),
				"cwd":        strProp("Working directory inside workspace (default: workspace root)."),
				"timeout":    intProp("Timeout in seconds (default 60, max 300).", defaultExecTimeoutSec),
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				action := argStr(args, "action")
				if action == "" {
					return errVal(fmt.Errorf("action is required"))
				}
				if action != "run" {
					return errVal(fmt.Errorf("unknown action: %s (only \"run\" is supported)", action))
				}
				dir, err := resolveWorkdir(ws, restrict, argStr(args, "cwd"))
				if err != nil {
					return errVal(err)
				}
				timeout := defaultExecTimeoutSec
				if _, ok := args["timeout"]; ok {
					timeout = int(argInt(args, "timeout"))
				}
				memMB := defaultExecMemMB
				if a != nil && a.Config != nil {
					memMB = clampMemMB(a.Config.ExecMemoryMB)
				}
				res := runExec(dir, argStr(args, "command"), clampTimeout(timeout), memMB)
				out := map[string]any{"success": res.Success, "exit_code": res.ExitCode, "output": res.Output}
				if res.TimedOut {
					out["timed_out"] = true
				}
				if res.MemoryLimited {
					out["memory_limited"] = true
				}
				return out, nil
			}),
		"telegram_sendfile": mk("telegram_sendfile", "Send a local file (image, document, etc.) to the user on the current chat channel. Only works in Telegram chat.",
			objSchema([]string{"path"}, map[string]any{
				"path":     strProp("Path to the local file. Relative paths are resolved from workspace."),
				"filename": strProp("Optional display filename. Defaults to the basename of path."),
				"caption":  strProp("Optional caption for the file."),
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				if a == nil || a.Telegram == nil || opts == nil || opts.ChatID == 0 {
					return map[string]any{"error": "telegram_sendfile hanya tersedia di chat Telegram"}, nil
				}
				abs, err := resolvePath(ws, restrict, argStr(args, "path"))
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				b, err := os.ReadFile(abs)
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				if len(b) > maxSendFileBytes {
					return map[string]any{"error": "file terlalu besar (maks 20MB)"}, nil
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
		"telegram_getuser": mk("telegram_getuser", "Get a Telegram user's name, id and info. No args = the user currently asking; pass user_id to look up any user live via the API. Only works in Telegram chat.",
			objSchema(nil, map[string]any{
				"user_id": map[string]any{"type": "number", "description": "Optional Telegram user id to look up (default: current requester)."},
			}),
			func(ctx context.Context, args map[string]any) (any, error) {
				if uid := argInt(args, "user_id"); uid != 0 {
					if a == nil || a.Telegram == nil {
						return map[string]any{"error": "telegram_getuser hanya tersedia di chat Telegram"}, nil
					}
					info, err := a.Telegram.GetTelegramUser(ctx, uid)
					if err != nil {
						return map[string]any{"error": err.Error()}, nil
					}
					return userInfoMap(info), nil
				}
				if opts == nil || opts.User == nil {
					return map[string]any{"error": "telegram_getuser hanya tersedia di chat Telegram"}, nil
				}
				u := opts.User
				return userInfoMap(&TelegramUserInfo{ID: u.ID, Username: u.Username, FirstName: u.FirstName, LastName: u.LastName}), nil
			}),
	}
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

func intProp(desc string, def int64) map[string]any {
	return map[string]any{"type": "integer", "description": desc, "default": def}
}

func boolProp(desc string, def bool) map[string]any {
	return map[string]any{"type": "boolean", "description": desc, "default": def}
}

func enumProp(desc string, values []string) map[string]any {
	return map[string]any{"type": "string", "enum": values, "description": desc}
}
