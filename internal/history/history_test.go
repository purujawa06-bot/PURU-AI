package history

import (
	"testing"

	"github.com/purujawa06-bot/PURU-AI/internal/messages"
)

func mkMsg(role, text string) *messages.Message {
	m := &messages.Message{Role: role}
	messages.SetContentString(m, text)
	return m
}

func TestSetGetClear(t *testing.T) {
	s := New(t.TempDir())
	msgs := []*messages.Message{mkMsg("user", "halo"), mkMsg("assistant", "hai")}
	if err := s.Set(1, msgs); err != nil {
		t.Fatal(err)
	}
	got := s.Get(1)
	if len(got) != 2 || got[0].Text() != "halo" {
		t.Fatalf("get mismatch: %v", got)
	}
	if err := s.Clear(1); err != nil {
		t.Fatal(err)
	}
	if got := s.Get(1); len(got) != 0 {
		t.Fatalf("expected wiped history, got %d msgs", len(got))
	}
}

func TestTokenCountGrows(t *testing.T) {
	a := []*messages.Message{mkMsg("user", "hi")}
	b := append([]*messages.Message{}, a...)
	b = append(b, mkMsg("user", "lorem ipsum dolor sit amet consectetur adipiscing elit sed do"))
	if TokenCount(b) <= TokenCount(a) {
		t.Fatalf("token count must grow with history")
	}
}
