package ai

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

// BuildTools returns 4 tools: 2 local workspace tools (edit_file, exec)
// + 2 Telegram tools (telegram_sendfile, telegram_getuser, only usable with
// a Telegram context). File reads/writes go through exec (cat, heredoc, …).
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
	return map[string]*Tool{
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
		"exec": mk("exec", "Run a shell command. Strictly capped: timeout 60s default (max 300s), RAM capped by exec_memory_mb (group killed when over), files capped 100MB, output truncated 20k chars.",
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
				memMB := defaultExecMemMB
				if a != nil && a.Config != nil {
					memMB = clampMemMB(a.Config.ExecMemoryMB)
				}
				res := runExec(dir, argStr(args, "command"), clampTimeout(args["timeout_seconds"]), memMB)
				out := map[string]any{"success": res.Success, "exit_code": res.ExitCode, "output": res.Output}
				if res.TimedOut {
					out["timed_out"] = true
				}
				if res.MemoryLimited {
					out["memory_limited"] = true
				}
				return out, nil
			}),
		"telegram_sendfile": mk("telegram_sendfile", "Send a workspace file to the current Telegram chat as a document. Only works in Telegram chat.",
			objSchema([]string{"path"}, map[string]any{
				"path":    strProp("Workspace-relative file path to send."),
				"caption": strProp("Optional caption for the file."),
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
				if err := a.Telegram.SendFile(ctx, opts.ChatID, filepath.Base(abs), b, argStr(args, "caption")); err != nil {
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
	}
	return 0
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
