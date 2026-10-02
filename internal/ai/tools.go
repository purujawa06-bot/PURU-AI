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
// list_dir, grep, edit_file, append_file) + run_shell_command + Telegram tools
// (telegram_sendfile, telegram_getuser) + get_env + web_fetch + manage_schedule
// + spawn_agent + skill tools (use_skill, stop_skill). web_search
// (third-party, opt-in: Google AI Studio with googleSearch grounding and/or
// Exa) is added as the 16th tool only when at least one web_search provider
// is ready (active + credentials) in config.json. No PuruBoy API anywhere.
// opts carries workspace config, current chat/user, and the OnTool preview hook.
func BuildTools(a *Agent, opts *ProcessOptions) map[string]*Tool {
	ws := ""
	restrict := true
	if a != nil && a.Config != nil {
		ws = a.Config.Workspace
		restrict = a.Config.RestrictWorkspace
	}
	mk := func(name string, run func(ctx context.Context, args map[string]any) (any, error)) *Tool {
		// Single source of truth: internal/ai/tools_schema.json.
		// Edit the JSON only to change what the model sees.
		desc := toolDescription(name)
		params := toolParameters(name)
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
			"read_file": mk("read_file",
			func(ctx context.Context, args map[string]any) (any, error) {
				// Active skills stay readable: use_skill remains the
				// preferred loader, read_file is kept for debugging.
				length := int64(defaultReadFileLines)
				if _, ok := args["length"]; ok {
					length = argInt(args, "length")
				}
				text, err := readLocalFile(ws, restrict, argStr(args, "path"), argInt(args, "start_line"), length)
				if err != nil {
					return errVal(err)
				}
				return text, nil
			}),
		"write_file": mk("write_file",
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
		"list_dir": mk("list_dir",
			func(ctx context.Context, args map[string]any) (any, error) {
				text, err := listLocalDir(ws, restrict, argStr(args, "path"))
				if err != nil {
					return errVal(err)
				}
				return text, nil
			}),
		"grep": mk("grep",
			func(ctx context.Context, args map[string]any) (any, error) {
				limit := int64(50)
				if _, ok := args["limit"]; ok {
					limit = argInt(args, "limit")
				}
				text, err := grepLocal(ws, restrict, argStr(args, "path"), argStr(args, "keyword"), argStr(args, "ext"), int(limit))
				if err != nil {
					return errVal(err)
				}
				return text, nil
			}),
		"edit_file": mk("edit_file",
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
		"append_file": mk("append_file",
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
			"run_shell_command": mk("run_shell_command",
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
		"telegram_sendfile": mk("telegram_sendfile",
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
		"telegram_getuser": mk("telegram_getuser",
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
		"get_env": mk("get_env",
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
		"web_fetch": mk("web_fetch",
			func(ctx context.Context, args map[string]any) (any, error) {
				text, err := runWebFetch(ctx, argStr(args, "url"), argStr(args, "section"), int(argInt(args, "start_line")), int(argInt(args, "length")))
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
		tools["web_search"] = mk("web_search",
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
	tools["manage_schedule"] = buildScheduleTool(a, opts, mk, errVal)
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






