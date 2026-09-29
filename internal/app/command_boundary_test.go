package app

import "testing"

// Command names end at the first non-word character, so longer words
// sharing a prefix ("/stopwatch") are plain user text for the agent.
func TestIsCommandBoundary(t *testing.T) {
	for _, c := range []string{"/help", "/help@bot", "/clear", "/token", "/stop", "/stop@bot", "/sched", "/sched remove abc", "/stop."} {
		if !isCommand(c) {
			t.Errorf("isCommand(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"/stopwatch", "/clearance", "/tokenize x", "/helpdesk", "/scheduler", "/start", "halo"} {
		if isCommand(c) {
			t.Errorf("isCommand(%q) = true, want false", c)
		}
	}
}
