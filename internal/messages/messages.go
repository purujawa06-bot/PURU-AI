// Package messages holds the conversation-message model used everywhere.
//
// The persisted JSON schema mirrors the Vercel AI SDK v7 ModelMessage format
// so history written by the previous TypeScript bot keeps working. Unknown
// top-level fields and unknown fields inside content parts are preserved on
// round-trips (no data loss).
package messages

import (
	"bytes"
	"encoding/json"
	"strings"
)

const MaxStoredContent = 8000

// Part is a single content part of a message. It is kept as a raw map so all
// provider-specific fields survive a round-trip through storage.
type Part map[string]json.RawMessage

func (p *Part) Str(key string) string {
	raw, ok := (*p)[key]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

func (p *Part) Type() string { return p.Str("type") }

func (p *Part) Text() string {
	if p.Str("type") == "text" {
		return p.Str("text")
	}
	return ""
}

func (p *Part) SetText(t string) { p.Set("text", t) }

func (p *Part) Set(key string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	(*p)[key] = b
}

// ToolCallText returns the text the provider receives for a tool-call part:
// the tool name plus its (canonical) JSON input arguments.
func (p *Part) ToolCallText() string {
	if p == nil || p.Type() != "tool-call" {
		return ""
	}
	name := p.Str("toolName")
	raw, ok := (*p)["input"]
	if !ok || len(raw) == 0 {
		return name
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return name
	}
	b, err := json.Marshal(v)
	if err != nil {
		return name
	}
	if name == "" {
		return string(b)
	}
	return name + " " + string(b)
}

// ResultText returns the text the provider receives for a tool-result part:
// the string value for text outputs, canonical JSON for json outputs.
func (p *Part) ResultText() string {
	if p == nil || p.Type() != "tool-result" {
		return ""
	}
	raw, ok := (*p)["output"]
	if !ok {
		return ""
	}
	var out struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return ""
	}
	var s string
	if json.Unmarshal(out.Value, &s) == nil {
		return s
	}
	var v any
	if err := json.Unmarshal(out.Value, &v); err == nil {
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
	}
	return strings.TrimSpace(string(out.Value))
}

// Message is a canonical model message. Unrecognized top-level JSON fields are
// kept in extras and re-emitted on marshal.
type Message struct {
	Role    string
	Content json.RawMessage
	extras  map[string]json.RawMessage
}

func (m *Message) MarshalJSON() ([]byte, error) {
	out := map[string]json.RawMessage{}
	if m.Role != "" {
		b, _ := json.Marshal(m.Role)
		out["role"] = b
	}
	if len(m.Content) > 0 {
		out["content"] = m.Content
	} else {
		out["content"] = json.RawMessage("null")
	}
	for k, v := range m.extras {
		out[k] = v
	}
	return json.Marshal(out)
}

func (m *Message) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if r, ok := raw["role"]; ok {
		var s string
		if json.Unmarshal(r, &s) == nil {
			m.Role = s
		}
		delete(raw, "role")
	}
	if r, ok := raw["content"]; ok {
		m.Content = r
		delete(raw, "content")
	}
	if len(raw) > 0 {
		m.extras = raw
	}
	return nil
}

// Extra returns a raw unknown top-level field (e.g. legacy "toolCalls",
// "toolCallId", "providerOptions"). Returns nil when absent.
func (m *Message) Extra(key string) json.RawMessage {
	if m == nil || m.extras == nil {
		return nil
	}
	return m.extras[key]
}

func (m *Message) SetExtra(key string, v any) {
	if m.extras == nil {
		m.extras = map[string]json.RawMessage{}
	}
	b, err := json.Marshal(v)
	if err == nil {
		m.extras[key] = b
	}
}

// ContentNull reports whether content is absent or null.
func ContentNull(m *Message) bool {
	return m == nil || len(m.Content) == 0 || bytes.Equal(m.Content, json.RawMessage("null"))
}

// ContentString returns the content when it is a plain string.
func ContentString(m *Message) (string, bool) {
	if m == nil || len(m.Content) == 0 {
		return "", false
	}
	var s string
	if err := json.Unmarshal(m.Content, &s); err != nil {
		return "", false
	}
	return s, true
}

func IsStringContent(m *Message) bool { _, ok := ContentString(m); return ok }

// ContentParts returns the content when it is an array of parts.
func ContentParts(m *Message) []Part {
	if m == nil || ContentNull(m) {
		return nil
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(m.Content, &parts); err != nil {
		return nil
	}
	out := make([]Part, 0, len(parts))
	for _, pr := range parts {
		var p Part
		if json.Unmarshal(pr, &p) == nil {
			out = append(out, p)
		}
	}
	return out
}

// IsParts reports whether content decodes to a JSON array of parts.
func IsParts(m *Message) bool {
	if m == nil || ContentNull(m) {
		return false
	}
	if IsStringContent(m) {
		return false
	}
	var arr []json.RawMessage
	return json.Unmarshal(m.Content, &arr) == nil
}

func SetContentString(m *Message, s string) {
	b, _ := json.Marshal(s)
	m.Content = b
}

func SetContentParts(m *Message, parts []Part) {
	if len(parts) == 0 {
		m.Content = nil
		return
	}
	raw := make([]json.RawMessage, 0, len(parts))
	for _, p := range parts {
		b, err := json.Marshal(p)
		if err != nil {
			continue
		}
		raw = append(raw, b)
	}
	b, _ := json.Marshal(raw)
	m.Content = b
}

func IsSystem(m *Message) bool    { return m != nil && m.Role == "system" }
func IsUser(m *Message) bool      { return m != nil && m.Role == "user" }
func IsAssistant(m *Message) bool { return m != nil && m.Role == "assistant" }
func IsTool(m *Message) bool      { return m != nil && m.Role == "tool" }

// Text extracts the plain text content of the message (string content or the
// concatenation of text parts).
func (m *Message) Text() string {
	if m == nil {
		return ""
	}
	if s, ok := ContentString(m); ok {
		return s
	}
	if IsParts(m) {
		var sb strings.Builder
		for _, p := range ContentParts(m) {
			if p.Type() == "text" {
				sb.WriteString(p.Text())
			}
		}
		return sb.String()
	}
	return ""
}

// NetLen returns the length used by pruneMessages' empty message removal: the
// length of a string content or the number of parts.
func NetLen(m *Message) int {
	if m == nil || ContentNull(m) {
		return 0
	}
	if s, ok := ContentString(m); ok {
		return len(s)
	}
	return len(ContentParts(m))
}

// ---------------------------------------------------------------------------
// Pruning is disabled: history is kept as-is (reasoning, tool-call,
// tool-result, and empty responses preserved exactly after each turn).
// Reduction only via compact (memory.Compact) when history_token_limit hits.
// PruneMessages/PruneTurn are kept as no-ops so old callers don't break.
// ---------------------------------------------------------------------------

// PruneMessages is a no-op: return the input as-is without dropping
// reasoning, tool parts, or empty messages.
func PruneMessages(msgs []*Message) []*Message {
	return msgs
}

// PruneTurn is a no-op: return the input as-is. Don't strip reasoning or
// drop empty messages — let compaction handle it when the limit hits.
func PruneTurn(msgs []*Message) []*Message {
	return msgs
}

// isEmptyStored reports messages with nothing worth storing: null content,
// whitespace-only string content, or zero parts (e.g. fully stripped above).
// Messages carrying legacy top-level toolCalls are never empty.
func isEmptyStored(m *Message) bool {
	if len(m.Extra("toolCalls")) > 0 {
		return false
	}
	if ContentNull(m) {
		return true
	}
	if s, ok := ContentString(m); ok {
		return strings.TrimSpace(s) == ""
	}
	if IsParts(m) {
		parts := ContentParts(m)
		if len(parts) == 0 {
			return true
		}
		for _, p := range parts {
			switch p.Type() {
			case "tool-call", "tool-result", "reasoning", "reasoning-file":
				return false
			case "text":
				if strings.TrimSpace(p.Text()) != "" {
					return false
				}
			default:
				// Unknown part types count as content.
				return false
			}
		}
		return true
	}
	return NetLen(m) == 0
}

// EnsureStartsWithUser guarantees the first non-system message is a user
// message (avoids API error 400). Leading system messages are preserved.
func EnsureStartsWithUser(msgs []*Message) []*Message {
	result := append([]*Message{}, msgs...)
	i := 0
	for i < len(result) && IsSystem(result[i]) {
		i++
	}
	for i < len(result) && !IsUser(result[i]) {
		result = append(result[:i], result[i+1:]...)
	}
	return result
}

// KeepLastExchange returns the last user text plus the last assistant text
// as [user, assistant] in chronological order. Used after compaction so
// the next request continues with live context instead of an empty history.
// When history ends with a user (no assistant answer yet) only that user
// is kept to guarantee the kept history starts with a user (avoids API 400).
// When no user text exists the result is empty (summary covers context).
// Tool messages are dropped to save tokens; the summary file covers older
// context. Returns empty when no user text messages exist.
func KeepLastExchange(msgs []*Message) []*Message {
	lastUser := -1
	lastAssist := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m == nil {
			continue
		}
		if lastAssist == -1 && IsAssistant(m) && strings.TrimSpace(m.Text()) != "" {
			lastAssist = i
		} else if lastUser == -1 && IsUser(m) && strings.TrimSpace(m.Text()) != "" {
			lastUser = i
		}
		if lastUser != -1 && lastAssist != -1 {
			break
		}
	}
	if lastUser == -1 {
		return []*Message{}
	}
	if lastAssist == -1 {
		return []*Message{msgs[lastUser]}
	}
	if lastUser < lastAssist {
		return []*Message{msgs[lastUser], msgs[lastAssist]}
	}
	return []*Message{msgs[lastUser]}
}

const MaxUserMessages = 5

// CapUserTurns keeps at most MaxUserMessages user turns. The incoming user
// message is appended at request time, so stored history keeps at most
// MaxUserMessages-1 turns. The oldest user turn is removed together with its
// assistant/tool responses.
func CapUserTurns(history []*Message) []*Message {
	result := append([]*Message{}, history...)
	countUser := func() int {
		n := 0
		for _, m := range result {
			if IsUser(m) {
				n++
			}
		}
		return n
	}
	for countUser() >= MaxUserMessages {
		first := -1
		for i, m := range result {
			if IsUser(m) {
				first = i
				break
			}
		}
		next := -1
		for i, m := range result {
			if i > first && IsUser(m) {
				next = i
				break
			}
		}
		if first < 0 || next < 0 {
			break
		}
		result = append(result[:first], result[next:]...)
	}
	return EnsureStartsWithUser(result)
}

// SanitizeHistoryMessages only caps sizes (8k chars per message/part)
// so large tool outputs don't pile up. It drops nothing: reasoning,
// tool-call/tool-result, and empty responses (whitespace/null) are
// preserved as-is so the agent can't hallucinate. Reduction only via compact.
func SanitizeHistoryMessages(msgs []*Message) []*Message {
	out := make([]*Message, 0, len(msgs))
	for _, m := range msgs {
		c := SanitizeMessage(m)
		if c == nil {
			continue
		}
		out = append(out, c)
	}
	return out
}

// isEmptyStub reports whether an assistant message carries no content worth
// persisting: whitespace-only text and no tool-call/tool-result/reasoning
// parts (e.g. the intermediate "\n" produced before a scold correction).
func isEmptyStub(m *Message) bool {
	if len(m.Extra("toolCalls")) > 0 {
		return false
	}
	if IsParts(m) {
		for _, p := range ContentParts(m) {
			switch p.Type() {
			case "tool-call", "tool-result", "reasoning", "reasoning-file":
				return false
			}
		}
	}
	if ContentNull(m) {
		return true
	}
	if s, ok := ContentString(m); ok {
		return strings.TrimSpace(s) == ""
	}
	return true
}

func truncateString(s string, max int) string {
	if len(s) > max {
		return s[:max] + "\n...[truncated]"
	}
	return s
}

func SanitizeMessage(m *Message) *Message {
	c := cloneMessage(m)
	if s, ok := ContentString(c); ok {
		SetContentString(c, truncateString(s, MaxStoredContent))
		return c
	}
	if IsParts(c) {
		parts := ContentParts(c)
		out := make([]Part, 0, len(parts))
		for i := range parts {
			p := &parts[i]
			switch p.Type() {
			case "text":
				p.SetText(truncateString(p.Text(), MaxStoredContent))
			case "reasoning", "reasoning-file":
				if t := p.Str("text"); t != "" {
					p.SetText(truncateString(t, MaxStoredContent))
				}
			case "tool-call":
				if raw, ok := (*p)["input"]; ok && len(raw) > MaxStoredContent {
					(*p)["input"] = json.RawMessage(`"[truncated tool input]"`)
				}
			case "tool-result":
				if raw, ok := (*p)["output"]; ok && len(raw) > MaxStoredContent {
					(*p)["output"] = json.RawMessage(`{"type":"json","value":"[truncated tool result]"}`)
				}
			}
			out = append(out, *p)
		}
		if len(out) == 0 {
			// Keep empty messages as-is (do not drop).
			return c
		}
		SetContentParts(c, out)
	}
	// Legacy v5/v6 top-level toolCalls with args.
	if tc := c.Extra("toolCalls"); len(tc) > 0 {
		var calls []map[string]json.RawMessage
		if json.Unmarshal(tc, &calls) == nil {
			for _, tcall := range calls {
				if raw, ok := tcall["args"]; ok && len(raw) > MaxStoredContent {
					tcall["args"] = json.RawMessage(`"[truncated tool args]"`)
				}
			}
			c.SetExtra("toolCalls", calls)
		}
	}
	return c
}

func cloneMessage(m *Message) *Message {
	if m == nil {
		return nil
	}
	n := &Message{Role: m.Role}
	if len(m.Content) > 0 {
		n.Content = append(json.RawMessage{}, m.Content...)
	}
	if m.extras != nil {
		n.extras = map[string]json.RawMessage{}
		for k, v := range m.extras {
			n.extras[k] = v
		}
	}
	return n
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
