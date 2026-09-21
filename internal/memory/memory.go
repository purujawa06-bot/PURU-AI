// Package memory: raw conversation dump for the lightweight assistant.
//
// Flow (no history trimming anywhere else):
//   - Before each new prompt, the app counts history tokens.
//   - If history >= HistoryTokenLimit (default 30k), Compact is called:
//     the raw messages are dumped as-is (JSON) into one file under
//     <workspace>/context/YYYY-MM-DD_HH-MM-SS.json, only the newest 20
//     files are kept, then history is wiped clean.
//   - The AI is NOT given the file content — only the file path is
//     injected as a system note, so it can read that file (or older ones
//     in context/) with exec (cat) when old context is needed.
//
// No model call happens here: dumping is instant and free.
// MEMORY.md is NEVER touched here: it holds lasting user facts
// (name, hobby, personal info) written by the agent itself.
package memory

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/purujawa06-bot/PURU-AI/internal/messages"
)

// MaxSummaries keeps only this many newest files in context/.
const MaxSummaries = 20

type Manager struct {
	Workspace string
}

func New(workspace string) *Manager {
	return &Manager{Workspace: workspace}
}

// ContextDir is <workspace>/context — where dump files live.
func (m *Manager) ContextDir() string { return filepath.Join(m.Workspace, "context") }

// Compact dumps msgs as raw JSON into context/YYYY-MM-DD_HH-MM-SS.json
// (date+time only), prunes old dumps to MaxSummaries newest, and returns
// the workspace-relative path (e.g. "context/2026-09-21_14-05-30.json").
// MEMORY.md is never touched. Returns "" when there is nothing to dump.
func (m *Manager) Compact(_ context.Context, msgs []*messages.Message) (string, error) {
	var live []*messages.Message
	for _, msg := range msgs {
		if msg != nil {
			live = append(live, msg)
		}
	}
	if len(live) == 0 {
		return "", nil
	}
	raw, err := json.MarshalIndent(live, "", "  ")
	if err != nil {
		return "", err
	}
	rel, err := m.saveDump(append(raw, '\n'))
	if err != nil {
		return "", err
	}
	m.prune()
	return rel, nil
}

// SummaryNote is the system note injected after compaction: path only,
func SummaryNote(rel string) string {
	return "Percakapan sebelumnya telah disimpan mentah di " + rel +
		". Baca file itu dengan exec (mis. cat) bila butuh konteks lama; file lama lain ada di folder context/."
}

// saveDump writes the file (deduping name collisions on same-second
// compactions) and returns the workspace-relative path with forward slashes.
func (m *Manager) saveDump(raw []byte) (string, error) {
	dir := m.ContextDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	stamp := time.Now().Format("2006-01-02_15-04-05")
	name := stamp + ".json"
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
			break
		}
		name = stamp + "-" + strconv.Itoa(i) + ".json"
	}
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
		return "", err
	}
	return "context/" + name, nil
}

// prune deletes oldest dumps beyond MaxSummaries newest (best-effort).
// Old .md summaries (from before the raw-dump era) count toward the cap
// too so they age out naturally.
func (m *Manager) prune() {
	entries, err := os.ReadDir(m.ContextDir())
	if err != nil {
		return
	}
	type fi struct {
		name string
		mod  time.Time
	}
	var files []fi
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || (!strings.HasSuffix(n, ".json") && !strings.HasSuffix(n, ".md")) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fi{n, info.ModTime()})
	}
	if len(files) <= MaxSummaries {
		return
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].mod.Equal(files[j].mod) {
			return files[i].name > files[j].name
		}
		return files[i].mod.After(files[j].mod)
	})
	for _, f := range files[MaxSummaries:] {
		if err := os.Remove(filepath.Join(m.ContextDir(), f.name)); err != nil {
			log.Printf("[memory] prune %s: %v", f.name, err)
		}
	}
}
