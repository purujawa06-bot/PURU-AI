package skillstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestActivateDeactivatePersists(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "skillstate"))
	if got := store.Get(7); len(got) != 0 {
		t.Fatalf("fresh store must be empty, got %v", got)
	}
	added, err := store.Activate(7, "find-skills")
	if err != nil || !added {
		t.Fatalf("activate = %v, %v", added, err)
	}
	if added, _ := store.Activate(7, "FIND-SKILLS"); added {
		t.Fatalf("duplicate activate must return false")
	}
	if !store.IsActive(7, "find-skills") {
		t.Fatalf("skill must be active")
	}
	// New store instance over the same dir must reload from disk.
	dir := store.Dir()
	reloaded := New(dir)
	if !reloaded.IsActive(7, "find-skills") {
		t.Fatalf("state must survive restart via disk")
	}
	removed, err := reloaded.Deactivate(7, "Find-Skills")
	if err != nil || !removed {
		t.Fatalf("deactivate = %v, %v", removed, err)
	}
	if reloaded.IsActive(7, "find-skills") {
		t.Fatalf("skill must be inactive after deactivate")
	}
}

func TestCorruptFileYieldsEmpty(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	path := filepath.Join(dir, "9.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := store.Get(9); len(got) != 0 {
		t.Fatalf("corrupt file must yield empty, got %v", got)
	}
}

func TestPruneKeepsAllowlist(t *testing.T) {
	store := New(t.TempDir())
	if _, err := store.Activate(3, "find-skills"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Activate(3, "gone-skill"); err != nil {
		t.Fatal(err)
	}
	if err := store.Prune(3, []string{"find-skills"}); err != nil {
		t.Fatal(err)
	}
	got := store.Get(3)
	if len(got) != 1 || got[0] != "find-skills" {
		t.Fatalf("prune must drop deleted skills, got %v", got)
	}
}
