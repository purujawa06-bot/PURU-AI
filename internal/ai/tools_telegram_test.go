package ai

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeSender struct {
	chatID   int64
	filename string
	data     []byte
	caption  string
	users    map[int64]*TelegramUserInfo
}

func (f *fakeSender) SendFile(ctx context.Context, chatID int64, filename string, data []byte, caption string) error {
	f.chatID, f.filename, f.data, f.caption = chatID, filename, data, caption
	return nil
}

func (f *fakeSender) GetTelegramUser(ctx context.Context, userID int64) (*TelegramUserInfo, error) {
	if u, ok := f.users[userID]; ok {
		return u, nil
	}
	return nil, errFakeUnknownUser
}

var errFakeUnknownUser = fakeUserError("user tidak dikenal")

type fakeUserError string

func (e fakeUserError) Error() string { return string(e) }

func TestTelegramToolsNeedContext(t *testing.T) {
	tools := BuildTools(testAgent(t.TempDir()), nil)
	ctx := context.Background()

	if r, _ := tools["telegram_getuser"].Run(ctx, map[string]any{}); !hasErr(r) {
		t.Errorf("getuser tanpa user harus error, got %v", r)
	}
	if r, _ := tools["telegram_sendfile"].Run(ctx, map[string]any{"path": "a.txt"}); !hasErr(r) {
		t.Errorf("sendfile tanpa sender harus error, got %v", r)
	}
}

func TestTelegramGetUser(t *testing.T) {
	a := testAgent(t.TempDir())
	opts := &ProcessOptions{ChatID: 7, User: &TelegramUser{ID: 7, Username: "budi", FirstName: "Budi"}}
	tools := BuildTools(a, opts)
	r, _ := tools["telegram_getuser"].Run(context.Background(), map[string]any{})
	m, _ := r.(map[string]any)
	if m["username"] != "budi" || m["first_name"] != "Budi" {
		t.Fatalf("got %v", r)
	}
}

func TestTelegramGetUserByID(t *testing.T) {
	a := testAgent(t.TempDir())
	a.Telegram = &fakeSender{users: map[int64]*TelegramUserInfo{
		123: {ID: 123, Username: "siti", FirstName: "Siti", LastName: "Ayu", Bio: "halo saya siti"},
	}}
	tools := BuildTools(a, &ProcessOptions{ChatID: 7})
	r, _ := tools["telegram_getuser"].Run(context.Background(), map[string]any{"user_id": float64(123)})
	m, _ := r.(map[string]any)
	if m["id"] != int64(123) || m["first_name"] != "Siti" || m["last_name"] != "Ayu" || m["bio"] != "halo saya siti" {
		t.Fatalf("got %v", r)
	}
	// user tak dikenal -> error value
	if r, _ := tools["telegram_getuser"].Run(context.Background(), map[string]any{"user_id": float64(999)}); !hasErr(r) {
		t.Errorf("unknown user harus error, got %v", r)
	}
	// user_id tanpa Telegram client -> error value
	toolsNoTG := BuildTools(testAgent(t.TempDir()), &ProcessOptions{ChatID: 7})
	if r, _ := toolsNoTG["telegram_getuser"].Run(context.Background(), map[string]any{"user_id": float64(123)}); !hasErr(r) {
		t.Errorf("tanpa client harus error, got %v", r)
	}
}

func TestTelegramSendFile(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "doc.txt"), []byte("isi file"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := testAgent(ws)
	sender := &fakeSender{}
	a.Telegram = sender
	opts := &ProcessOptions{ChatID: 9}
	tools := BuildTools(a, opts)
	r, _ := tools["telegram_sendfile"].Run(context.Background(), map[string]any{"path": "doc.txt", "caption": "nih"})
	if m, _ := r.(map[string]any); m["success"] != true {
		t.Fatalf("sendfile failed: %v", r)
	}
	if sender.chatID != 9 || sender.filename != "doc.txt" || string(sender.data) != "isi file" || sender.caption != "nih" {
		t.Fatalf("sender got %+v", sender)
	}
	// escape tetap ditolak
	if r, _ := tools["telegram_sendfile"].Run(context.Background(), map[string]any{"path": "../x.txt"}); !hasErr(r) {
		t.Errorf("sendfile escape harus error, got %v", r)
	}
}

func TestOnToolHookFires(t *testing.T) {
	a := testAgent(t.TempDir())
	var calls []string
	opts := &ProcessOptions{OnTool: func(name string, args map[string]any) { calls = append(calls, name) }}
	tools := BuildTools(a, opts)
	ctx := context.Background()
	_, _ = tools["exec"].Run(ctx, map[string]any{"action": "run", "command": "echo hook"})
	_, _ = tools["edit_file"].Run(ctx, map[string]any{"path": "h.txt", "old_text": "x", "new_text": "y"})
	if len(calls) != 2 || calls[0] != "exec" || calls[1] != "edit_file" {
		t.Fatalf("hook calls = %v", calls)
	}
}

func TestLoopDelayDefault(t *testing.T) {
	a := &Agent{}
	if d := a.loopDelay(); d.String() != "3s" {
		t.Fatalf("loopDelay default = %v, want 3s", d)
	}
}

func hasErr(v any) bool {
	m, _ := v.(map[string]any)
	if m == nil {
		return false
	}
	e, _ := m["error"].(string)
	return strings.TrimSpace(e) != ""
}
