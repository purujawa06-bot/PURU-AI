package tokens

import (
	"encoding/json"
	"testing"

	"github.com/purujawa06-bot/PURU-AI/internal/messages"
)

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func toolResultMsg(payload string) *messages.Message {
	m := &messages.Message{Role: "tool"}
	messages.SetContentParts(m, []messages.Part{{
		"type":       mustJSON("tool-result"),
		"toolCallId": mustJSON("c1"),
		"toolName":   mustJSON("read_file"),
		"output":     mustJSON(map[string]any{"type": "json", "value": payload}),
	}})
	return m
}

func toolCallMsg() *messages.Message {
	m := &messages.Message{Role: "assistant"}
	messages.SetContentParts(m, []messages.Part{
		{"type": mustJSON("reasoning"), "text": mustJSON("pengguna ingin baca file, pakai read_file")},
		{"type": mustJSON("tool-call"), "toolCallId": mustJSON("c1"),
			"toolName": mustJSON("read_file"), "input": mustJSON(map[string]any{"path": "catatan/penting.txt"})},
	})
	return m
}

// Tool-result outputs must be counted (previously role "tool" was skipped).
func TestCountMessageToolResult(t *testing.T) {
	text := "baris rahasia file dengan banyak kata agar token jelas terhitung"
	got := CountMessage(toolResultMsg(text))
	if got < Count(text) {
		t.Fatalf("tool result tidak dihitung: got %d, want >= %d", got, Count(text))
	}
}

// Tool-call name+args and reasoning must be counted (previously skipped).
func TestCountMessageToolCall(t *testing.T) {
	got := CountMessage(toolCallMsg())
	min := Count("read_file") + Count(`{"path":"catatan/penting.txt"}`) +
		Count("pengguna ingin baca file, pakai read_file")
	if got < min {
		t.Fatalf("tool-call/reasoning tidak dihitung: got %d, want >= %d", got, min)
	}
}

// System prompt + framing must be part of the request estimate.
func TestCountRequestIncludesSystem(t *testing.T) {
	sys := "You are PURU-AI, a helpful local assistant with workspace tools"
	user := &messages.Message{Role: "user"}
	messages.SetContentString(user, "halo")
	msgs := []*messages.Message{user, toolResultMsg("ok")}
	full := CountRequest(sys, msgs, "pertanyaan baru")
	withoutSys := CountRequest("", msgs, "pertanyaan baru")
	if full-withoutSys < Count(sys) {
		t.Fatalf("system prompt tidak dihitung: diff %d < %d", full-withoutSys, Count(sys))
	}
	if withoutSys != CountConversation(msgs)+messageOverhead+Count("pertanyaan baru") {
		t.Fatalf("komposisi CountRequest salah: got %d", withoutSys)
	}
}

// User/assistant text still counted as before (no regression).
func TestCountConversationText(t *testing.T) {
	a := &messages.Message{Role: "user"}
	messages.SetContentString(a, "halo dunia")
	b := &messages.Message{Role: "assistant"}
	messages.SetContentString(b, "halo juga")
	got := CountConversation([]*messages.Message{a, b})
	want := 2*messageOverhead + Count("halo dunia") + Count("halo juga")
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}
