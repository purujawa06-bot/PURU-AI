// Package skillstate tracks runtime-active skills per chat.
//
// Files: ~/.puru/skillstate/{chatID}.json (JSON object with active names).
// Only names are stored, never skill bodies, so SKILL.md edits take effect
// on the next prompt build without re-activation. In-memory cache mirrors
// the history.Store pattern: fast per-request reads, write-through on change.
package skillstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Store persists active skill names per chat with an in-memory cache.
type Store struct {
	dir string
	mu  sync.Mutex
	mem map[string][]string
}

// stateFile is the JSON shape on disk.
type stateFile struct {
	Active    []string `json:"active"`
	UpdatedAt string   `json:"updated_at"`
}

// New returns a Store rooted at dir (e.g. ~/.puru/skillstate).
func New(dir string) *Store {
	return &Store{dir: dir, mem: map[string][]string{}}
}

// Dir returns the filesystem root backing this store (used in tests).
func (s *Store) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

func (s *Store) key(chatID int64) string {
	return strconv.FormatInt(chatID, 10)
}

func (s *Store) path(chatID int64) string {
	return filepath.Join(s.dir, s.key(chatID)+".json")
}

// normalize trims, drops empties, and dedupes case-insensitively,
// returning names sorted case-insensitively for stable output.
func normalize(names []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		folded := strings.ToLower(trimmed)
		if _, ok := seen[folded]; ok {
			continue
		}
		seen[folded] = struct{}{}
		out = append(out, trimmed)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

// Get returns a copy of the active skill names for chatID.
// Missing or corrupt files yield nil so prompt builds never fail.
func (s *Store) Get(chatID int64) []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.key(chatID)
	if names, ok := s.mem[key]; ok {
		return append([]string(nil), names...)
	}
	raw, err := os.ReadFile(s.path(chatID))
	if err != nil || len(raw) == 0 {
		return nil
	}
	var state stateFile
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil
	}
	names := normalize(state.Active)
	s.mem[key] = names
	return append([]string(nil), names...)
}

// IsActive reports whether name is active for chatID (case-insensitive).
func (s *Store) IsActive(chatID int64, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, active := range s.Get(chatID) {
		if strings.EqualFold(active, name) {
			return true
		}
	}
	return false
}

// saveLocked writes mem entry to disk atomically (tmp + rename).
func (s *Store) saveLocked(chatID int64) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	key := s.key(chatID)
	state := stateFile{
		Active:    append([]string(nil), s.mem[key]...),
		UpdatedAt: time.Now().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(chatID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(chatID))
}

// Activate adds name to the active set; returns true when it was added.
func (s *Store) Activate(chatID int64, name string) (bool, error) {
	if s == nil {
		return false, nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.key(chatID)
	current, ok := s.mem[key]
	if !ok {
		current = s.loadLocked(chatID)
	}
	for _, existing := range current {
		if strings.EqualFold(existing, name) {
			return false, nil
		}
	}
	s.mem[key] = normalize(append(append([]string(nil), current...), name))
	return true, s.saveLocked(chatID)
}

// Deactivate removes name from the active set; returns true when removed.
func (s *Store) Deactivate(chatID int64, name string) (bool, error) {
	if s == nil {
		return false, nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.key(chatID)
	current, ok := s.mem[key]
	if !ok {
		current = s.loadLocked(chatID)
	}
	kept := make([]string, 0, len(current))
	removed := false
	for _, existing := range current {
		if strings.EqualFold(existing, name) {
			removed = true
			continue
		}
		kept = append(kept, existing)
	}
	if !removed {
		return false, nil
	}
	s.mem[key] = normalize(kept)
	return true, s.saveLocked(chatID)
}

// Prune keeps only names present in keep (case-insensitive).
// Used to drop deleted or policy-blocked skills after restart.
func (s *Store) Prune(chatID int64, keep []string) error {
	if s == nil {
		return nil
	}
	allowed := map[string]struct{}{}
	for _, name := range keep {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			allowed[strings.ToLower(trimmed)] = struct{}{}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.key(chatID)
	current, ok := s.mem[key]
	if !ok {
		current = s.loadLocked(chatID)
	}
	kept := make([]string, 0, len(current))
	for _, existing := range current {
		if _, ok := allowed[strings.ToLower(existing)]; ok {
			kept = append(kept, existing)
		}
	}
	// Normalize first so corrupt on-disk order does not force a rewrite.
	if equalFoldSet(current, kept) {
		s.mem[key] = normalize(current)
		return nil
	}
	s.mem[key] = normalize(kept)
	return s.saveLocked(chatID)
}

// loadLocked reads disk without locking (caller holds mu).
func (s *Store) loadLocked(chatID int64) []string {
	raw, err := os.ReadFile(s.path(chatID))
	if err != nil || len(raw) == 0 {
		return nil
	}
	var state stateFile
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil
	}
	return normalize(state.Active)
}

func equalFoldSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := map[string]int{}
	for _, name := range a {
		counts[strings.ToLower(strings.TrimSpace(name))]++
	}
	for _, name := range b {
		key := strings.ToLower(strings.TrimSpace(name))
		if counts[key] == 0 {
			return false
		}
		counts[key]--
	}
	return true
}
