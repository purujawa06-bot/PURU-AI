// Package history: lightweight local per-chat history.
//
// Files: ~/.puru/history/{chatID}.json (JSON array of messages).
// No Firebase, no cache TTL complexity — small in-memory map + disk.
// History is NEVER trimmed here; the app layer summarizes it with the model
// into a context/*.md file when the token limit is hit, wipes history, and
// the newest summary is injected into the system prompt.
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/purujawa06-bot/PURU-AI/internal/messages"
	"github.com/purujawa06-bot/PURU-AI/internal/tokens"
)

type Store struct {
	dir string
	mu  sync.Mutex
	mem map[string][]*messages.Message
}

func New(dir string) *Store {
	return &Store{dir: dir, mem: map[string][]*messages.Message{}}
}

func (s *Store) path(chatID int64) string {
	return filepath.Join(s.dir, fmt.Sprintf("%d.json", chatID))
}

func (s *Store) Get(chatID int64) []*messages.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprint(chatID)
	if m, ok := s.mem[key]; ok {
		return append([]*messages.Message{}, m...)
	}
	raw, err := os.ReadFile(s.path(chatID))
	if err != nil || len(raw) == 0 {
		return nil
	}
	var msgs []*messages.Message
	if json.Unmarshal(raw, &msgs) != nil {
		return nil
	}
	s.mem[key] = msgs
	return append([]*messages.Message{}, msgs...)
}

func (s *Store) Set(chatID int64, msgs []*messages.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprint(chatID)
	cp := append([]*messages.Message{}, msgs...)
	s.mem[key] = cp
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(chatID), b, 0o644)
}

// Clear wipes history totally (used after memory compaction).
func (s *Store) Clear(chatID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.mem, fmt.Sprint(chatID))
	if err := os.Remove(s.path(chatID)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// TokenCount estimates stored history size (all roles and tool payloads).
func TokenCount(msgs []*messages.Message) int {
	return tokens.CountConversation(msgs)
}

// TokenCountFull estimates the full request context: rendered system prompt
// + stored history. Used by the compaction trigger and /token so the number
// matches what the model actually receives.
func TokenCountFull(system string, msgs []*messages.Message) int {
	return tokens.CountRequest(system, msgs, "")
}
