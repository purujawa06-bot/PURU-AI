package messages

import (
	"encoding/json"
	"strings"
	"testing"
)

func makeMsg(role, text string) *Message {
	m := &Message{Role: role}
	SetContentString(m, text)
	return m
}

func TestRoundTripPreservesExtras(t *testing.T) {
	raw := []byte(`[{"role":"assistant","content":[],"toolCalls":[{"id":"1","function":{"name":"search","arguments":"{\"q\":\"x\"}"}}]},{"role":"tool","content":[{"type":"tool-result","toolCallId":"1","toolName":"search","output":{"type":"json","value":{"ok":1}}}],"providerOptions":{}}]`)
	var msgs []*Message
	if err := json.Unmarshal(raw, &msgs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(msgs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"toolCalls"`) {
		t.Fatalf("round trip lost toolCalls: %s", string(out))
	}
	if !strings.Contains(string(out), `"providerOptions"`) {
		t.Fatalf("round trip lost providerOptions: %s", string(out))
	}
}

func TestCapUserTurns(t *testing.T) {
	var history []*Message
	for _, pair := range [][2]string{
		{"user", "u1"}, {"assistant", "a1"},
		{"user", "u2"}, {"assistant", "a2"},
		{"user", "u3"}, {"assistant", "a3"},
		{"user", "u4"}, {"assistant", "a4"},
		{"user", "u5"}, {"assistant", "a5"},
	} {
		history = append(history, makeMsg(pair[0], pair[1]))
	}
	got := CapUserTurns(history)
	users := 0
	for _, m := range got {
		if m.Role == "user" {
			users++
		}
	}
	if users >= MaxUserMessages {
		t.Fatalf("CapUserTurns kept %d user turns", users)
	}
	if r := firstNonSystemRole(got); r != "user" {
		t.Fatalf("expected to start with user, got %q", r)
	}
}

func TestEnsureStartsWithUser(t *testing.T) {
	in := []*Message{
		{Role: "system"},
		makeMsg("assistant", "x"),
		makeMsg("user", "y"),
	}
	out := EnsureStartsWithUser(in)
	if out[0].Role != "system" || len(out) != 2 {
		t.Fatalf("unexpected ensure result: %+v", out)
	}
	out = EnsureStartsWithUser(out)
	if firstNonSystemRole(out) != "user" {
		t.Fatalf("expect user first, got %s", firstNonSystemRole(out))
	}
}

func TestSanitizeTruncatesText(t *testing.T) {
	big := strings.Repeat("x", 10000)
	truncated := SanitizeMessage(makeMsg("user", big))
	s, ok := ContentString(truncated)
	if !ok || len(s) > MaxStoredContent+len("\n...[truncated]") {
		t.Fatalf("sanitize did not truncate: length=%d", len(s))
	}
}

func TestSanitizeDropsEmptyTextParts(t *testing.T) {
	m := &Message{Role: "assistant"}
	SetContentParts(m, []Part{
		{"type": []byte(`"text"`), "text": []byte(`"\n"`)},
		{"type": []byte(`"tool-call"`), "toolCallId": []byte(`"c1"`), "toolName": []byte(`"finish"`), "input": []byte(`{}`)},
	})
	got := SanitizeMessage(m)
	parts := ContentParts(got)
	// Prune dinonaktifkan: text kosong + tool-call dipertahankan apa adanya.
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts preserved apa adanya, got %d parts", len(parts))
	}
}

// Prune dinonaktifkan: Sanitize hanya truncate, tidak drop stub/empty agar
// agent tidak halusinasi. Biarkan compact yang bekerja saat kena limit.
func TestSanitizeHistoryDropsStubs(t *testing.T) {
	partStub := &Message{Role: "assistant"}
	SetContentParts(partStub, []Part{{"type": []byte(`"text"`), "text": []byte(`"\n"`)}})

	stringStub := makeMsg("assistant", " \n ")

	kept := &Message{Role: "assistant"}
	SetContentParts(kept, []Part{
		{"type": []byte(`"tool-call"`), "toolCallId": []byte(`"c1"`), "toolName": []byte(`"finish"`), "input": []byte(`{}`)},
	})

	out := SanitizeHistoryMessages([]*Message{partStub, stringStub, kept, makeMsg("user", "hi")})
	if len(out) != 4 {
		t.Fatalf("expected 4 messages preserved apa adanya, got %d", len(out))
	}
}

// TestPruneKeepsReasoning verifies that PruneMessages (no-op) preserves
// reasoning parts apa adanya.
func TestPruneKeepsReasoning(t *testing.T) {
	assistant := &Message{Role: "assistant"}
	SetContentParts(assistant, []Part{
		{"type": []byte(`"reasoning"`), "text": []byte(`"old turn thinking"`)},
		{"type": []byte(`"tool-call"`), "toolCallId": []byte(`"c1"`), "toolName": []byte(`"x"`), "input": []byte(`{}`)},
	})
	tool := &Message{Role: "tool"}
	SetContentParts(tool, []Part{{"type": []byte(`"tool-result"`), "toolCallId": []byte(`"c1"`), "toolName": []byte(`"x"`), "output": []byte(`{}`)}})
	history := []*Message{
		makeMsg("user", "u1"),
		assistant,
		tool,
		makeMsg("user", "u2"),
		makeMsg("assistant", "a2"),
	}

	out := PruneMessages(history)
	found := false
	for _, m := range out {
		if m.Role != "assistant" || !IsParts(m) {
			continue
		}
		for _, p := range ContentParts(m) {
			if p.Type() == "reasoning" && p.Str("text") == "old turn thinking" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("PruneMessages dropped reasoning from an older assistant message")
	}
}

func TestSanitizeTruncatesReasoning(t *testing.T) {
	big := strings.Repeat("y", 10000)
	raw, _ := json.Marshal(big)
	m := &Message{Role: "assistant"}
	SetContentParts(m, []Part{{"type": []byte(`"reasoning"`), "text": raw}})

	got := SanitizeMessage(m)
	parts := ContentParts(got)
	if len(parts) != 1 || parts[0].Type() != "reasoning" {
		t.Fatalf("expected a single reasoning part, got %d parts", len(parts))
	}
	if got := parts[0].Str("text"); len(got) > MaxStoredContent+len("\n...[truncated]") {
		t.Fatalf("sanitize did not truncate reasoning: length=%d", len(got))
	}
}

func firstNonSystemRole(msgs []*Message) string {
	for _, m := range msgs {
		if m.Role != "system" {
			return m.Role
		}
	}
	return ""
}

func mkParts(role string, parts []Part) *Message {
	m := &Message{Role: role}
	SetContentParts(m, parts)
	return m
}

func hasPartType(m *Message, typ string) bool {
	if m == nil || !IsParts(m) {
		return false
	}
	for _, p := range ContentParts(m) {
		if p.Type() == typ {
			return true
		}
	}
	return false
}

func hasToolID(m *Message, typ, id string) bool {
	if m == nil || !IsParts(m) {
		return false
	}
	for _, p := range ContentParts(m) {
		if p.Type() == typ && p.Str("toolCallId") == id {
			return true
		}
	}
	return false
}

// TestPruneTurn verifies PruneTurn is a no-op: reasoning, tool parts, dan
// pesan kosong dipertahankan apa adanya (biarkan compact yang bekerja).
func TestPruneTurn(t *testing.T) {
	msgs := []*Message{
		mkParts("assistant", []Part{
			{"type": []byte(`"reasoning"`), "text": []byte(`"old-0"`)},
			{"type": []byte(`"text"`), "text": []byte(`"t0"`)},
		}),
		mkParts("assistant", []Part{
			{"type": []byte(`"text"`), "text": []byte(`"t1"`)},
			{"type": []byte(`"tool-call"`), "toolCallId": []byte(`"c-old"`), "toolName": []byte(`"x"`), "input": []byte(`{}`)},
		}),
		mkParts("tool", []Part{
			{"type": []byte(`"tool-result"`), "toolCallId": []byte(`"r-old"`), "toolName": []byte(`"x"`), "output": []byte(`{"type":"text","value":"old"}`)},
		}),
		mkParts("assistant", []Part{
			{"type": []byte(`"reasoning"`), "text": []byte(`"old-3"`)},
			{"type": []byte(`"text"`), "text": []byte(`"t3"`)},
		}),
		makeMsg("user", "u4"),
		mkParts("assistant", []Part{
			{"type": []byte(`"text"`), "text": []byte(`"t5"`)},
			{"type": []byte(`"reasoning"`), "text": []byte(`"newest"`)},
		}),
		makeMsg("user", "u6"),
		mkParts("assistant", []Part{
			{"type": []byte(`"text"`), "text": []byte(`"t7"`)},
			{"type": []byte(`"tool-call"`), "toolCallId": []byte(`"c-new"`), "toolName": []byte(`"x"`), "input": []byte(`{}`)},
		}),
		mkParts("tool", []Part{
			{"type": []byte(`"tool-result"`), "toolCallId": []byte(`"r-new"`), "toolName": []byte(`"x"`), "output": []byte(`{"type":"text","value":"new"}`)},
		}),
		makeMsg("user", "   "),
		{Role: "assistant"},
	}
	snap, _ := json.Marshal(msgs)

	out := PruneTurn(msgs)

	// Input tidak boleh termutasi.
	after, _ := json.Marshal(msgs)
	if string(snap) != string(after) {
		t.Fatalf("PruneTurn mutated input")
	}

	// Apa adanya: jumlah sama, semua reasoning/tool/empty dipertahankan.
	if len(out) != len(msgs) {
		t.Fatalf("PruneTurn harus no-op: got %d, want %d", len(out), len(msgs))
	}
	if hasToolID(nil, "tool-call", "x") {
		t.Fatal("helper sanity failed")
	}

	// Reasoning: semua 3 dipertahankan (old-0, old-3, newest).
	reasonCount := 0
	for _, m := range out {
		if !IsParts(m) {
			continue
		}
		for _, p := range ContentParts(m) {
			if p.Type() == "reasoning" || p.Type() == "reasoning-file" {
				reasonCount++
			}
		}
	}
	if reasonCount != 3 {
		t.Fatalf("expected 3 reasoning parts preserved, got %d", reasonCount)
	}

	// Tool parts lama + baru dipertahankan.
	foundOldCall, foundOldResult, foundNewCall, foundNewResult := false, false, false, false
	for _, m := range out {
		if hasToolID(m, "tool-call", "c-old") {
			foundOldCall = true
		}
		if hasToolID(m, "tool-result", "r-old") {
			foundOldResult = true
		}
		if hasToolID(m, "tool-call", "c-new") {
			foundNewCall = true
		}
		if hasToolID(m, "tool-result", "r-new") {
			foundNewResult = true
		}
	}
	if !foundOldCall || !foundOldResult || !foundNewCall || !foundNewResult {
		t.Fatal("semua tool-call/tool-result harus dipertahankan apa adanya")
	}

	// Pesan kosong (whitespace + null) dipertahankan.
	emptyKept := 0
	for _, m := range out {
		if isEmptyStored(m) {
			emptyKept++
		}
	}
	if emptyKept < 2 {
		t.Fatalf("expected empty messages preserved, got %d", emptyKept)
	}
}
