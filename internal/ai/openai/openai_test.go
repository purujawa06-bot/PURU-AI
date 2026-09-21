package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tmc/langchaingo/llms"
)

// sseServer answers /chat/completions with a fixed text/event-stream body.
func sseServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(body))
	}))
}

// frag is a single tool_call fragment in a streamed delta.
type frag struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

// deltaLine builds an SSE "data:" line whose delta carries the given
// tool_calls fragments (argument-only). Marshalled so quotes are correct.
func deltaLine(args []string) string {
	var fs []frag
	for _, a := range args {
		f := frag{}
		f.Function.Arguments = a
		fs = append(fs, f)
	}
	return marshalDeltaLine(fs)
}

// deltaLineWithID builds the first tool-call fragment carrying id+type+name.
func deltaLineWithID(id, name string) string {
	f := frag{ID: id, Type: "function"}
	f.Function.Name = name
	return marshalDeltaLine([]frag{f})
}

// deltaLineIdx builds a fragment at the given index carrying type+arguments
// (the HF-style fragment that repeats type:"function").
func deltaLineIdx(idx *int, typ, args string) string {
	f := frag{Index: idx, Type: typ}
	f.Function.Arguments = args
	return marshalDeltaLine([]frag{f})
}

func marshalDeltaLine(fs []frag) string {
	type choice struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []frag `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	}
	payload := struct {
		Choices []choice `json:"choices"`
	}{
		Choices: []choice{{}},
	}
	p := &payload.Choices[0]
	p.FinishReason = "tool_calls"
	p.Delta.ToolCalls = fs
	b, _ := json.Marshal(payload)
	return "data: " + string(b)
}

// TestToolCallAssemblyHF verifies the HF Spaces gateway regression: every
// argument fragment repeats type:"function" (not the empty type langchaingo
// expects). They must all merge into one tool call with full arguments.
func TestToolCallAssemblyHF(t *testing.T) {
	// Simulates the HF gateway: each delta's tool_calls entry carries a full
	// index+type+function object, including argument-only fragments.
	idx0 := 0
	first := deltaLineWithID("call_1", "edit_file")
	mid := deltaLineIdx(&idx0, "function", `{"path":"a`)
	tail := deltaLineIdx(&idx0, "function", `bs.txt","content":"hello"}`)
	fin := deltaLine(nil)
	sse := strings.Join([]string{first, mid, tail, fin, `data: [DONE]`}, "\n\n")

	srv := sseServer(t, sse)
	defer srv.Close()

	client, err := New(srv.URL, "test", "test", srv.Client())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var streamed []string
	resp, err := client.GenerateContent(context.Background(),
		[]llms.MessageContent{{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "halo"}}}},
		llms.WithStreamingFunc(func(_ context.Context, chunk []byte) error {
			streamed = append(streamed, string(chunk))
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	tcs := resp.Choices[0].ToolCalls
	if len(tcs) != 1 {
		t.Fatalf("expected exactly 1 assembled tool call, got %d: %+v", len(tcs), tcs)
	}
	tc := tcs[0]
	if tc.ID != "call_1" {
		t.Errorf("unexpected id: %q", tc.ID)
	}
	if tc.FunctionCall == nil || tc.FunctionCall.Name != "edit_file" {
		t.Errorf("unexpected function name: %+v", tc.FunctionCall)
	}
	wantArgs := `{"path":"abs.txt","content":"hello"}`
	if tc.FunctionCall == nil || tc.FunctionCall.Arguments != wantArgs {
		t.Errorf("arguments = %q, want %q", tc.FunctionCall.Arguments, wantArgs)
	}
	// A pure tool-call response must not stream any visible content.
	for _, s := range streamed {
		if strings.TrimSpace(s) != "" {
			t.Errorf("unexpected streamed content: %q", s)
		}
	}
}

// TestToolCallAssemblyOpenAI verifies the canonical OpenAI-style stream where
// the first fragment carries name/id and later fragments only arguments.
func TestToolCallAssemblyOpenAI(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"role":"assistant","content":"","tool_calls":[{"index":0,"id":"call_9","type":"function","function":{"name":"search_web","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":\"ok"}}]},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"}"}}]},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	}, "\n\n")

	srv := sseServer(t, sse)
	defer srv.Close()

	client, _ := New(srv.URL, "test", "test", srv.Client())
	resp, err := client.GenerateContent(context.Background(),
		[]llms.MessageContent{{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "halo"}}}},
		llms.WithStreamingFunc(func(_ context.Context, _ []byte) error { return nil }),
	)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	tcs := resp.Choices[0].ToolCalls
	if len(tcs) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(tcs))
	}
	if got := tcs[0].FunctionCall.Arguments; got != `{"q":"ok"}` {
		t.Errorf("arguments = %q, want %q", got, `{"q":"ok"}`)
	}
}

// TestToolCallAssemblyMultiple verifies two concurrent tool calls assemble
// independently by index.
func TestToolCallAssemblyMultiple(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c0","type":"function","function":{"name":"a","arguments":"{\"x\":1}"}},{"index":1,"id":"c1","type":"function","function":{"name":"b","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"y\":2}"}}]},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	}, "\n\n")

	srv := sseServer(t, sse)
	defer srv.Close()

	client, _ := New(srv.URL, "test", "test", srv.Client())
	resp, err := client.GenerateContent(context.Background(),
		[]llms.MessageContent{{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "halo"}}}},
		llms.WithStreamingFunc(func(_ context.Context, _ []byte) error { return nil }),
	)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	tcs := resp.Choices[0].ToolCalls
	if len(tcs) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(tcs))
	}
	if tcs[0].FunctionCall.Name != "a" || tcs[0].FunctionCall.Arguments != `{"x":1}` {
		t.Errorf("tool[0] = %+v", tcs[0])
	}
	if tcs[1].FunctionCall.Name != "b" || tcs[1].FunctionCall.Arguments != `{"y":2}` {
		t.Errorf("tool[1] = %+v", tcs[1])
	}
}

// TestSSECommentsAndUsage verifies comment lines are skipped and the usage
// chunk is captured.
func TestSSECommentsAndUsage(t *testing.T) {
	sse := strings.Join([]string{
		`: OPENROUTER PROCESSING`,
		`data: {"choices":[{"delta":{"role":"assistant","content":"Hel"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"content":"lo"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`,
		`data: [DONE]`,
	}, "\n\n")

	srv := sseServer(t, sse)
	defer srv.Close()

	client, _ := New(srv.URL, "test", "test", srv.Client())
	resp, err := client.GenerateContent(context.Background(),
		[]llms.MessageContent{{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "halo"}}}},
		llms.WithStreamingFunc(func(_ context.Context, _ []byte) error { return nil }),
	)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if got := resp.Choices[0].Content; got != "Hello" {
		t.Errorf("content = %q, want %q", got, "Hello")
	}
	gi := resp.Choices[0].GenerationInfo
	if gi["TotalTokens"] != 12 || gi["PromptTokens"] != 10 || gi["CompletionTokens"] != 2 {
		t.Errorf("usage = %v", gi)
	}
}

// TestNonStreamFallback verifies that a proxy answering with plain JSON
// despite stream:true is handled (single JSON body).
func TestNonStreamFallback(t *testing.T) {
	jsonResp := `{"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(jsonResp))
	}))
	defer srv.Close()

	client, _ := New(srv.URL, "test", "test", srv.Client())
	resp, err := client.GenerateContent(context.Background(),
		[]llms.MessageContent{{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "halo"}}}},
		llms.WithStreamingFunc(func(_ context.Context, _ []byte) error { return nil }),
	)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if got := resp.Choices[0].Content; got != "ok" {
		t.Errorf("content = %q, want ok", got)
	}
}

// TestRequestStreamFlag verifies the wire request carries stream:true and the
// tools array when a StreamingFunc is set.
func TestRequestStreamFlag(t *testing.T) {
	var req wireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client, _ := New(srv.URL, "test", "test", srv.Client())
	_, err := client.GenerateContent(context.Background(),
		[]llms.MessageContent{{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "halo"}}}},
		llms.WithStreamingFunc(func(_ context.Context, _ []byte) error { return nil }),
		llms.WithFunctions([]llms.FunctionDefinition{{Name: "test_tool", Parameters: map[string]any{"type": "object"}}}),
	)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if !req.Stream {
		t.Error("stream flag not set on wire request")
	}
	if len(req.Tools) != 1 || req.Tools[0].Function.Name != "test_tool" {
		t.Errorf("tools not sent: %+v", req.Tools)
	}
}

// TestBuildMessagesToolCall verifies the outbound serialization of an
// assistant message containing tool calls.
func TestBuildMessagesToolCall(t *testing.T) {
	msgs := buildMessages([]llms.MessageContent{
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: "thinking"},
				llms.ToolCall{
					ID:           "call_1",
					Type:         "function",
					FunctionCall: &llms.FunctionCall{Name: "edit_file", Arguments: `{"path":"a.txt"}`},
				},
			},
		},
		{
			Role: llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{
				llms.ToolCallResponse{ToolCallID: "call_1", Content: "ok"},
			},
		},
	}, nil)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" || msgs[0].Content != "thinking" {
		t.Errorf("assistant msg = %+v", msgs[0])
	}
	if len(msgs[0].ToolCalls) != 1 || msgs[0].ToolCalls[0].Function.Name != "edit_file" {
		t.Errorf("assistant tool calls = %+v", msgs[0].ToolCalls)
	}
	if msgs[1].Role != "tool" || msgs[1].ToolCallID != "call_1" || msgs[1].Content != "ok" {
		t.Errorf("tool msg = %+v", msgs[1])
	}
}

// TestSSEReasoningContent verifies the client captures reasoning_content from a
// streamed thinking-mode response (deepseek-reasoner style deltas).
func TestSSEReasoningContent(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"role":"assistant","reasoning_content":"R1","content":""},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"reasoning_content":"R2","content":"final"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	}, "\n\n")

	srv := sseServer(t, sse)
	defer srv.Close()

	client, _ := New(srv.URL, "test", "test", srv.Client())
	resp, err := client.GenerateContent(context.Background(),
		[]llms.MessageContent{{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "halo"}}}},
		llms.WithStreamingFunc(func(_ context.Context, _ []byte) error { return nil }),
	)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if got := resp.Choices[0].ReasoningContent; got != "R1R2" {
		t.Errorf("reasoning_content = %q, want R1R2", got)
	}
	if got := resp.Choices[0].Content; got != "final" {
		t.Errorf("content = %q, want final", got)
	}
	if got := resp.Choices[0].GenerationInfo["ThinkingContent"]; got != "R1R2" {
		t.Errorf("ThinkingContent = %v, want R1R2", got)
	}
}

// TestNonStreamReasoningContent verifies a proxy answering with plain JSON
// (despite stream:true) still surfaces reasoning_content.
func TestNonStreamReasoningContent(t *testing.T) {
	jsonResp := `{"choices":[{"index":0,"message":{"role":"assistant","content":"ok","reasoning_content":"step-by-step"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(jsonResp))
	}))
	defer srv.Close()

	client, _ := New(srv.URL, "test", "test", srv.Client())
	resp, err := client.GenerateContent(context.Background(),
		[]llms.MessageContent{{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "halo"}}}},
		llms.WithStreamingFunc(func(_ context.Context, _ []byte) error { return nil }),
	)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if got := resp.Choices[0].ReasoningContent; got != "step-by-step" {
		t.Errorf("reasoning_content = %q, want step-by-step", got)
	}
}

// TestGenerateContentWithReasoning verifies that a thinking-mode assistant
// message's reasoning_content is echoed back on the wire (index-aligned), and
// that GenerateContent without reasoning emits none.
func TestGenerateContentWithReasoning(t *testing.T) {
	var req wireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client, _ := New(srv.URL, "test", "test", srv.Client())

	msgs := []llms.MessageContent{
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "halo"}}},
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: "jawaban"},
				llms.ToolCall{ID: "c1", Type: "function", FunctionCall: &llms.FunctionCall{Name: "x", Arguments: `{}`}},
			},
		},
		{Role: llms.ChatMessageTypeTool, Parts: []llms.ContentPart{llms.ToolCallResponse{ToolCallID: "c1", Content: "ok"}}},
	}

	_, err := client.GenerateContentWithReasoning(context.Background(), msgs,
		[]string{"", "R1", ""},
		llms.WithStreamingFunc(func(_ context.Context, _ []byte) error { return nil }),
	)
	if err != nil {
		t.Fatalf("GenerateContentWithReasoning: %v", err)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("expected 3 wire messages, got %d", len(req.Messages))
	}
	if req.Messages[0].ReasoningContent != "" {
		t.Errorf("user message unexpectedly got reasoning: %q", req.Messages[0].ReasoningContent)
	}
	if got := req.Messages[1].ReasoningContent; got != "R1" {
		t.Errorf("assistant reasoning_content = %q, want R1", got)
	}
	if req.Messages[2].ReasoningContent != "" {
		t.Errorf("tool message unexpectedly got reasoning: %q", req.Messages[2].ReasoningContent)
	}

	// Plain GenerateContent must not emit reasoning_content at all.
	req = wireRequest{}
	_, err = client.GenerateContent(context.Background(), msgs,
		llms.WithStreamingFunc(func(_ context.Context, _ []byte) error { return nil }),
	)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	for i, m := range req.Messages {
		if m.ReasoningContent != "" {
			t.Errorf("plain GenerateContent emitted reasoning at %d: %q", i, m.ReasoningContent)
		}
	}
}

// TestContentToStringImage verifies that a message with a binary (image) part
// serializes to the OpenAI vision content array with a base64 data URI, while a
// text-only message stays a plain string.
func TestContentToStringImage(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x01, 0x02, 0x03}

	m := &wireMessage{}
	contentToString([]llms.ContentPart{
		llms.TextContent{Text: "apa isi gambar ini?"},
		llms.BinaryContent{MIMEType: "image/png", Data: png},
	}, m)

	raw, err := json.Marshal(m.Content)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wantURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	if got := string(raw); !strings.Contains(got, `"type":"text"`) ||
		!strings.Contains(got, `"type":"image_url"`) ||
		!strings.Contains(got, wantURL) {
		t.Fatalf("content = %s, want vision array containing %s", got, wantURL)
	}

	// MIMEType empty -> sniffed from magic bytes.
	m2 := &wireMessage{}
	contentToString([]llms.ContentPart{llms.BinaryContent{Data: png}}, m2)
	raw2, _ := json.Marshal(m2.Content)
	if !strings.Contains(string(raw2), "data:image/png;base64,") {
		t.Fatalf("sniffed content = %s", raw2)
	}

	// Non-image binary falls back to octet-stream.
	m3 := &wireMessage{}
	contentToString([]llms.ContentPart{llms.BinaryContent{Data: []byte("hello")}}, m3)
	raw3, _ := json.Marshal(m3.Content)
	if !strings.Contains(string(raw3), "data:application/octet-stream;base64,") {
		t.Fatalf("octet-stream content = %s", raw3)
	}

	// Text-only stays a plain string.
	m4 := &wireMessage{}
	contentToString([]llms.ContentPart{llms.TextContent{Text: "halo"}}, m4)
	if m4.Content != "halo" {
		t.Fatalf("text-only content = %v, want string halo", m4.Content)
	}
}

// TestExtraHeadersAndSessionMarker verifies that extra headers are sent on the
// wire, "@session" is substituted with the stable per-chat session id, and
// "@request" gets a fresh msg_ id per call.
func TestExtraHeadersAndSessionMarker(t *testing.T) {
	var captured map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = map[string]string{
			"x-opencode-client":  r.Header.Get("x-opencode-client"),
			"x-opencode-session": r.Header.Get("x-opencode-session"),
			"x-opencode-request": r.Header.Get("x-opencode-request"),
			"x-opencode-project": r.Header.Get("x-opencode-project"),
			"User-Agent":         r.Header.Get("User-Agent"),
			"Authorization":      r.Header.Get("Authorization"),
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client, err := NewWithOptions(Options{
		BaseURL: srv.URL,
		APIKey:  "public",
		Model:   "deepseek-v4-flash-free",
		Session: "ses_abc123",
		Headers: map[string]string{
			"x-opencode-client":  "desktop",
			"x-opencode-session": "@session",
			"x-opencode-request": "@request",
			"x-opencode-project": "global",
			"User-Agent":         "opencode",
		},
	}, srv.Client())
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	msgs := []llms.MessageContent{{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "halo"}}}}
	_, err = client.GenerateContent(context.Background(), msgs,
		llms.WithStreamingFunc(func(_ context.Context, _ []byte) error { return nil }),
	)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if captured["x-opencode-client"] != "desktop" || captured["x-opencode-session"] != "ses_abc123" ||
		captured["x-opencode-project"] != "global" || captured["User-Agent"] != "opencode" {
		t.Fatalf("headers wrong: %+v", captured)
	}
	if captured["Authorization"] != "Bearer public" {
		t.Fatalf("auth header wrong: %q", captured["Authorization"])
	}
	firstReq := captured["x-opencode-request"]
	if !strings.HasPrefix(firstReq, "msg_") {
		t.Fatalf("request id must start with msg_: %q", firstReq)
	}

	// Second call: same session, fresh request id.
	captured = nil
	_, err = client.GenerateContent(context.Background(), msgs,
		llms.WithStreamingFunc(func(_ context.Context, _ []byte) error { return nil }),
	)
	if err != nil {
		t.Fatalf("GenerateContent #2: %v", err)
	}
	if captured["x-opencode-session"] != "ses_abc123" {
		t.Fatalf("session must be stable across calls: %q", captured["x-opencode-session"])
	}
	if captured["x-opencode-request"] == firstReq || captured["x-opencode-request"] == "" {
		t.Fatalf("request id must be fresh per call: first=%q second=%q", firstReq, captured["x-opencode-request"])
	}
}

// TestRelayProxy verifies that a configured relay receives the request at the
// relay root with x-relay-target / x-relay-path headers (the 9router protocol)
// while all other headers and the body are preserved.
func TestRelayProxy(t *testing.T) {
	var got struct {
		Path    string
		Target  string
		PathHdr string
		Auth    string
		Body    wireRequest
	}
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Path = r.URL.Path
		got.Target = r.Header.Get("x-relay-target")
		got.PathHdr = r.Header.Get("x-relay-path")
		got.Auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer relay.Close()

	origin := httptest.NewServer(http.NotFoundHandler())
	defer origin.Close()

	client, err := NewWithOptions(Options{
		BaseURL: origin.URL + "/v1",
		APIKey:  "public",
		Model:   "deepseek-v4-flash-free",
		Proxy:   relay.URL,
	}, relay.Client())
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}

	_, err = client.GenerateContent(context.Background(),
		[]llms.MessageContent{{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "halo"}}}},
		llms.WithStreamingFunc(func(_ context.Context, _ []byte) error { return nil }),
	)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if got.Path != "/" && got.Path != "" {
		t.Fatalf("relay request must hit the relay root, got path %q", got.Path)
	}
	if got.Target != origin.URL {
		t.Fatalf("x-relay-target = %q, want %q", got.Target, origin.URL)
	}
	if got.PathHdr != "/v1/chat/completions" {
		t.Fatalf("x-relay-path = %q, want /v1/chat/completions", got.PathHdr)
	}
	if got.Auth != "Bearer public" {
		t.Fatalf("Authorization not forwarded: %q", got.Auth)
	}
	if got.Body.Model != "deepseek-v4-flash-free" || !got.Body.Stream {
		t.Fatalf("relay body wrong: %+v", got.Body)
	}
}

// TestRelayProxyIgnoresOrigin verifies the relay is actually used: the origin
// server must never receive a request when a relay is configured.
func TestRelayProxyIgnoresOrigin(t *testing.T) {
	originHit := false
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHit = true
		w.Write([]byte("unexpected"))
	}))
	defer origin.Close()

	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer relay.Close()

	client, _ := NewWithOptions(Options{BaseURL: origin.URL, Proxy: relay.URL}, relay.Client())
	_, err := client.GenerateContent(context.Background(),
		[]llms.MessageContent{{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "halo"}}}},
		llms.WithStreamingFunc(func(_ context.Context, _ []byte) error { return nil }),
	)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if originHit {
		t.Fatal("origin must not receive any request when a relay is configured")
	}
}
