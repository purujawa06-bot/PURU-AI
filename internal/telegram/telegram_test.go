package telegram

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSanitizeText(t *testing.T) {
	got := sanitizeText("halo \xff\xfe dunia")
	if got != "halo \ufffd dunia" {
		t.Fatalf("unexpected sanitize result: %q", got)
	}
}

func TestSanitizeTextKeepsValidUTF8(t *testing.T) {
	in := "Halo 😀 — judul: Gunung Everest, paragraf pertama: Mount Everest"
	if got := sanitizeText(in); got != in {
		t.Fatalf("valid UTF-8 must be unchanged, got %q", got)
	}
}

// fakeBot serves canned Bot API responses for methodPath.
func fakeBot(t *testing.T, methodPath, resp string, code int) (*API, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, methodPath) {
			t.Errorf("unexpected path %q, want suffix %q", r.URL.Path, methodPath)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(resp))
	}))
	api, err := newWithServer("123456:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return api, srv.Close
}

func TestGetMe(t *testing.T) {
	api, done := fakeBot(t, "/getMe", `{"ok":true,"result":{"id":123,"is_bot":true,"username":"purubot"}}`, 200)
	defer done()
	u, err := api.GetMe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != 123 || u.Username != "purubot" {
		t.Fatalf("got %+v", u)
	}
}

func TestSendMessageReturnsID(t *testing.T) {
	api, done := fakeBot(t, "/sendMessage", `{"ok":true,"result":{"message_id":42,"date":1,"chat":{"id":8,"type":"private"},"text":"hi"}}`, 200)
	defer done()
	id, err := api.SendMessage(context.Background(), 8, "hi", map[string]any{
		"parse_mode":          "Markdown",
		"reply_to_message_id": int64(7),
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != 42 {
		t.Fatalf("message id = %d, want 42", id)
	}
}

func TestGetUpdatesMapsTypes(t *testing.T) {
	api, done := fakeBot(t, "/getUpdates", `{"ok":true,"result":[{"update_id":5,"message":{"message_id":1,"from":{"id":7,"username":"u"},"chat":{"id":8,"type":"private"},"date":1,"text":"halo"}}]}`, 200)
	defer done()
	updates, err := api.GetUpdates(context.Background(), 0, 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(updates))
	}
	u := updates[0]
	if u.UpdateID != 5 || u.Message == nil || u.Message.Text != "halo" {
		t.Fatalf("got %+v", u)
	}
	if u.Message.From.ID != 7 || u.Message.Chat.ID != 8 {
		t.Fatalf("got %+v", u.Message)
	}
}

func TestConflictWrapped(t *testing.T) {
	api, done := fakeBot(t, "/getUpdates", `{"ok":false,"error_code":409,"description":"Conflict: terminated by other getUpdates request"}`, 200)
	defer done()
	_, err := api.GetUpdates(context.Background(), 0, 40)
	if err == nil {
		t.Fatalf("expected conflict error")
	}
	var te *TelegramError
	if !errors.As(err, &te) {
		t.Fatalf("expected TelegramError, got %T: %v", err, err)
	}
	if te.Code != 409 || !te.IsConflict() {
		t.Fatalf("got %+v", te)
	}
}

func TestParseEntitiesWrapped(t *testing.T) {
	api, done := fakeBot(t, "/sendMessage", `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`, 200)
	defer done()
	_, err := api.SendMessage(context.Background(), 8, "x", map[string]any{"parse_mode": "Markdown"})
	var te *TelegramError
	if !errors.As(err, &te) {
		t.Fatalf("expected TelegramError, got %T: %v", err, err)
	}
	if te.Code != 400 || !strings.Contains(te.Message, "parse entities") {
		t.Fatalf("got %+v", te)
	}
}
