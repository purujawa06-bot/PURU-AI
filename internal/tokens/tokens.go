// Package tokens wraps tiktoken (o200k_base, matching js-tiktoken) for the
// /token command and history token estimation.
//
// The counters mirror exactly what is sent to the model on every request
// (see ai.toChatHistory + the rendered system prompt): system prompt, user
// text, assistant text, tool-call names+args, tool-result outputs and
// replayed reasoning — plus a small per-message framing overhead.
package tokens

import (
	"encoding/json"
	"sync"

	"github.com/tiktoken-go/tokenizer"

	"github.com/purujawa06-bot/PURU-AI/internal/messages"
)

var (
	once sync.Once
	enc  tokenizer.Codec
)

func initEncoder() {
	enc, _ = tokenizer.Get(tokenizer.O200kBase)
}

// messageOverhead approximates the framing tokens the API charges per chat
// message (role tags etc.). Follows OpenAI's token-counting cookbook rule
// (tokens_per_message) so estimates stay close to billed usage.
const messageOverhead = 3

// Count returns the number of tokens for a UTF-8 string using o200k_base.
// Falls back to a byte-length estimate if the encoder is unavailable.
func Count(s string) int {
	once.Do(initEncoder)
	if enc == nil {
		return len(s)
	}
	ids, _, _ := enc.Encode(s)
	return len(ids)
}

// CountMessage counts every token the model sees for one stored message:
// string content or all parts (text, reasoning, tool-call name+args,
// tool-result output), legacy top-level toolCalls, plus framing overhead.
func CountMessage(m *messages.Message) int {
	if m == nil {
		return 0
	}
	total := messageOverhead
	if s, ok := messages.ContentString(m); ok {
		return total + Count(s)
	}
	if messages.IsParts(m) {
		for _, p := range messages.ContentParts(m) {
			switch p.Type() {
			case "tool-call":
				total += Count(p.ToolCallText())
			case "tool-result":
				total += Count(p.ResultText())
			default:
				// text, reasoning, reasoning-file, ... carry a "text" field.
				if t := p.Str("text"); t != "" {
					total += Count(t)
				}
			}
		}
	}
	// Legacy v5/v6 top-level toolCalls (also replayed to the provider).
	if raw := m.Extra("toolCalls"); len(raw) > 0 {
		var calls []struct {
			Function struct {
				Name      string `json:"name"`
				Arguments any    `json:"arguments"`
			} `json:"function"`
		}
		if json.Unmarshal(raw, &calls) == nil {
			for _, c := range calls {
				total += Count(c.Function.Name)
				if b, err := json.Marshal(c.Function.Arguments); err == nil {
					total += Count(string(b))
				}
			}
		}
	}
	return total
}

// CountConversation sums CountMessage over stored history (all roles:
// system notes, user, assistant and tool results).
func CountConversation(msgs []*messages.Message) int {
	total := 0
	for _, m := range msgs {
		total += CountMessage(m)
	}
	return total
}

// CountRequest estimates the full context of one model call: the rendered
// system prompt + stored history + the current user input. This is what the
// compaction trigger and /token compare against history_token_limit.
func CountRequest(system string, msgs []*messages.Message, input string) int {
	total := CountConversation(msgs)
	if system != "" {
		total += messageOverhead + Count(system)
	}
	if input != "" {
		total += messageOverhead + Count(input)
	}
	return total
}

// CountConvTokens is kept for compatibility; it counts the full stored
// conversation (all roles and tool payloads), not just user/assistant text.
func CountConvTokens(msgs []*messages.Message) int {
	return CountConversation(msgs)
}
