package main

import (
	"bufio"
	"strings"
	"testing"
)

func TestDetectColorsHonorsEnv(t *testing.T) {
	t.Setenv("TERM", "")
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "1")
	if detectColors() {
		t.Fatal("NO_COLOR must win over FORCE_COLOR")
	}
	t.Setenv("NO_COLOR", "")
	if !detectColors() {
		t.Fatal("FORCE_COLOR must enable colors")
	}
}

func TestMaskSecret(t *testing.T) {
	cases := map[string]string{
		"":                    "(not set)",
		"  ":                  "(not set)",
		"short":               "*****",
		"sk-abcdef1234567890": "sk-a****7890",
	}
	for in, want := range cases {
		if got := maskSecret(in); got != want {
			t.Fatalf("maskSecret(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestQuestionFormat(t *testing.T) {
	prev := colorsEnabled
	colorsEnabled = false
	defer func() { colorsEnabled = prev }()

	if got, want := question("Model name", "gpt-4o-mini"), "? Model name [gpt-4o-mini]: "; got != want {
		t.Fatalf("question with default = %q, want %q", got, want)
	}
	if got, want := question("API key", ""), "? API key: "; got != want {
		t.Fatalf("question without default = %q, want %q", got, want)
	}
}

func TestAskLineRequiredReasksUntilAnswered(t *testing.T) {
	in := bufio.NewReader(strings.NewReader("\n  \n123:abc\n"))
	if got := askLine(in, "Bot token", "", true); got != "123:abc" {
		t.Fatalf("expected answer after re-ask, got %q", got)
	}
}

func TestAskLineEOFKeepsEmptyInsteadOfLooping(t *testing.T) {
	in := bufio.NewReader(strings.NewReader(""))
	if got := askLine(in, "Bot token", "", true); got != "" {
		t.Fatalf("expected empty on EOF, got %q", got)
	}
}

func TestAskIntFallsBackOnInvalidInput(t *testing.T) {
	in := bufio.NewReader(strings.NewReader("abc\n\n42\n"))
	if got := askInt(in, "Max iterations", 500); got != 500 {
		t.Fatalf("expected default on invalid input, got %d", got)
	}
	if got := askInt(in, "Max iterations", 500); got != 500 {
		t.Fatalf("expected default on empty input, got %d", got)
	}
	if got := askInt(in, "Max iterations", 500); got != 42 {
		t.Fatalf("expected parsed value, got %d", got)
	}
}

func TestAskInt64ListKeepsValidIDsOnly(t *testing.T) {
	in := bufio.NewReader(strings.NewReader("7, oops , 8\n"))
	got := askInt64List(in, "Allowed user IDs", nil)
	if len(got) != 2 || got[0] != 7 || got[1] != 8 {
		t.Fatalf("unexpected list: %v", got)
	}
}

func TestAskStringListSplitsAndTrims(t *testing.T) {
	in := bufio.NewReader(strings.NewReader(" find-skills , skill-creator ,\n"))
	got := askStringList(in, "Allowlist", nil)
	if len(got) != 2 || got[0] != "find-skills" || got[1] != "skill-creator" {
		t.Fatalf("unexpected list: %v", got)
	}
}
