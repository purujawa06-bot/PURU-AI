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
// pruneMessages port (options used by this project):
//   toolCalls  -> 'before-last-6-messages'
//   emptyMessages -> remove
//
// Reasoning parts are NEVER pruned: thinking-mode providers (deepseek-reasoner
// and friends) require every replayed assistant message to carry its
// reasoning_content back — dropping it makes the API return HTTP 400.
// ---------------------------------------------------------------------------

// PruneMessages mirrors Vercel's ai.pruneMessages with the configuration the
// bot uses. It never mutates its input.
func PruneMessages(msgs []*Message) []*Message {
	work := make([]*Message, 0, len(msgs))
	for _, m := range msgs {
		work = append(work, cloneMessage(m))
	}

	// toolCalls: 'before-last-6-messages'
	const keepLast = 6
	kept := map[string]struct{}{}
	start := len(work) - minInt(keepLast, len(work))
	if start < 0 {
		start = 0
	}
	for _, m := range work[start:] {
		if (m.Role == "assistant" || m.Role == "tool") && IsParts(m) {
			for _, p := range ContentParts(m) {
				if p.Type() == "tool-call" || p.Type() == "tool-result" {
					if id := p.Str("toolCallId"); id != "" {
						kept[id] = struct{}{}
					}
				}
			}
		}
	}
	for i, m := range work {
		if (m.Role != "assistant" && m.Role != "tool") || !IsParts(m) {
			continue
		}
		if i >= len(work)-keepLast {
			continue
		}
		parts := make([]Part, 0, len(ContentParts(m)))
		for _, p := range ContentParts(m) {
			t := p.Type()
			keep := true
			if t == "tool-call" || t == "tool-result" {
				_, keep = kept[p.Str("toolCallId")]
			}
			if keep {
				parts = append(parts, p)
			}
		}
		SetContentParts(m, parts)
	}

	// emptyMessages: remove
	out := make([]*Message, 0, len(work))
	for _, m := range work {
		if m != nil && NetLen(m) > 0 {
			out = append(out, m)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// PruneTurn: per-turn trim called once per completed turn after
// SanitizeHistoryMessages and before hist.Set (see app.processMessage and
// cmd/cli process). Single pass, no loops that can hang:
//
//   - tool-call/tool-result parts are kept only on the last 6 messages,
//     older messages keep their text/reasoning (reasoning then reduced below).
//   - only the single newest reasoning/reasoning-file part is kept globally.
//   - empty messages (null, whitespace-only string, or zero parts after the
//     stripping above) are removed.
//
// It never mutates its input. Compaction/memory logic is untouched — this only
// bounds per-turn tool/reasoning growth so history stays small until the token
// trigger summarizes it.
// ---------------------------------------------------------------------------

// PruneTurn trims one completed turn for storage.
func PruneTurn(msgs []*Message) []*Message {
	if len(msgs) == 0 {
		return msgs
	}
	work := make([]*Message, 0, len(msgs))
	for _, m := range msgs {
		work = append(work, cloneMessage(m))
	}
	const keepLast = 6
	threshold := len(work) - keepLast
	if threshold < 0 {
		threshold = 0
	}

	// Locate the single newest reasoning part (scan from the end).
	newestMsg := -1
	newestPart := -1
	for i := len(work) - 1; i >= 0; i-- {
		m := work[i]
		if m == nil || !IsParts(m) {
			continue
		}
		parts := ContentParts(m)
		for j := len(parts) - 1; j >= 0; j-- {
			if t := parts[j].Type(); t == "reasoning" || t == "reasoning-file" {
				newestMsg = i
				newestPart = j
				break
			}
		}
		if newestMsg >= 0 {
			break
		}
	}

	// Strip old tool parts and all but the newest reasoning part.
	for i, m := range work {
		if m == nil || !IsParts(m) {
			continue
		}
		parts := ContentParts(m)
		kept := make([]Part, 0, len(parts))
		for j, p := range parts {
			switch p.Type() {
			case "tool-call", "tool-result":
				if i < threshold {
					continue
				}
				kept = append(kept, p)
			case "reasoning", "reasoning-file":
				if i == newestMsg && j == newestPart {
					kept = append(kept, p)
				}
				// else: drop older reasoning
			default:
				kept = append(kept, p)
			}
		}
		SetContentParts(m, kept)
	}

	// Drop empty messages.
	out := make([]*Message, 0, len(work))
	for _, m := range work {
		if m == nil || isEmptyStored(m) {
			continue
		}
		out = append(out, m)
	}
	return out
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

// SanitizeHistoryMessages truncates stored message content (8k char limit per
// message/part) so large tool outputs do not pile up in cache or Firebase. It
// also drops assistant stubs that carry no real content (e.g. empty/whitespace
// text parts from interrupted protocol-violation rounds).
func SanitizeHistoryMessages(msgs []*Message) []*Message {
	out := make([]*Message, 0, len(msgs))
	for _, m := range msgs {
		c := SanitizeMessage(m)
		if c == nil || (IsAssistant(c) && isEmptyStub(c)) {
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
				t := p.Text()
				if strings.TrimSpace(t) == "" {
					continue // drop empty/whitespace text parts
				}
				p.SetText(truncateString(t, MaxStoredContent))
			case "reasoning":
				if t := p.Str("text"); t != "" {
					p.SetText(truncateString(t, MaxStoredContent))
				}
			case "tool-call":
				if raw, ok := (*p)["input"]; ok && len(raw) > MaxStoredContent {
					(*p)["input"] = json.RawMessage(`"[truncated tool input]"`)
				}
			}
			out = append(out, *p)
		}
		if len(out) == 0 {
			c.Content = nil
		} else {
			SetContentParts(c, out)
		}
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
