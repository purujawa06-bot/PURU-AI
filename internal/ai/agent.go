// Package ai implements the lightweight local tool-calling agent.
//
// Single model from config.json, 11 tools (read_file, write_file, list_dir,
// edit_file, append_file, exec, telegram_sendfile, telegram_getuser, get_env,
// web_search, web_fetch), no
// fallback: one executor run per request, max iterations from config (default
// 500), pause between iterations from config (loop_delay_seconds, default 3s).
// Every model call streams, and API errors are retried up to 5x total (2s
// delay) per call — failed tools are never re-run because retries happen
// inside the model call, not the run.
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/tmc/langchaingo/agents"
	"github.com/tmc/langchaingo/chains"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/prompts"
	"github.com/tmc/langchaingo/schema"
	"github.com/tmc/langchaingo/tools"

	"github.com/purujawa06-bot/PURU-AI/internal/config"
	"github.com/purujawa06-bot/PURU-AI/internal/memory"
	"github.com/purujawa06-bot/PURU-AI/internal/messages"
	"github.com/purujawa06-bot/PURU-AI/internal/prompt"
)

const (
	toolTimeout    = 330 * time.Second
	totalAgentTime = 20 * time.Minute
)

var errNoModel = errors.New("no AI model configured")

// reasoningClient is implemented by models that echo thinking-mode
// reasoning_content back to the provider.
type reasoningClient interface {
	llms.Model
	GenerateContentWithReasoning(ctx context.Context, messages []llms.MessageContent, reasonings []string, options ...llms.CallOption) (*llms.ContentResponse, error)
}

const stepLimitHint = "Step limit reached (max_iterations). Type `continue` to carry on."

// Usage is the token usage of a single model round-trip.
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// Tool is a registered tool implementation for a single request.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
	Run         func(ctx context.Context, args map[string]any) (any, error)
}

// Agent owns one model + local workspace. No per-user overrides.
type Agent struct {
	Client   llms.Model
	Config   *config.Config
	HTTP     *http.Client
	Telegram TelegramClient
	// ToolsBuild overrides tool construction (default BuildTools).
	ToolsBuild func(opts *ProcessOptions) (map[string]*Tool, error)
}

type ProcessOptions struct {
	ChatID int64
	// User is the Telegram requester (nil in CLI).
	User *TelegramUser
	// OnTool fires synchronously on every tool call (live preview hook).
	OnTool func(name string, args map[string]any)
}

type ProcessResult struct {
	Text             string
	ResponseMessages []*messages.Message
	TotalTokens      int
	LastStepUsage    Usage
	LastFinishReason string
}

type runResult struct {
	finalText        string
	responseMessages []*messages.Message
	totalTokens      int
	lastStepUsage    Usage
	lastFinishReason string
	hitStepLimit     bool
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// toolResultText delegates to messages so the token counter and the request
// builder always interpret tool-result parts identically.
func toolResultText(p *messages.Part) string {
	return p.ResultText()
}

func responseFromSteps(steps []schema.AgentStep, reasoningByStep []string) []*messages.Message {
	if len(steps) == 0 {
		return nil
	}
	out := make([]*messages.Message, 0, 2*len(steps))
	for i := 0; i < len(steps); {
		j := i
		for j < len(steps) && steps[j].Action.Log == steps[i].Action.Log {
			j++
		}
		group := steps[i:j]
		var parts []messages.Part
		if reasoning := reasoningForStep(reasoningByStep, i); strings.TrimSpace(reasoning) != "" {
			parts = append(parts, messages.Part{
				"type": mustJSON("reasoning"),
				"text": mustJSON(reasoning),
			})
		}
		if ll := strings.TrimSpace(group[0].Action.Log); ll != "" {
			parts = append(parts, messages.Part{
				"type": mustJSON("text"),
				"text": mustJSON(ll),
			})
		}
		for _, s := range group {
			var input any
			if err := json.Unmarshal([]byte(s.Action.ToolInput), &input); err != nil {
				input = map[string]any{}
			}
			parts = append(parts, messages.Part{
				"type":       mustJSON("tool-call"),
				"toolCallId": mustJSON(s.Action.ToolID),
				"toolName":   mustJSON(s.Action.Tool),
				"input":      mustJSON(input),
			})
		}
		as := &messages.Message{Role: "assistant"}
		messages.SetContentParts(as, parts)
		// Pertahankan apa adanya termasuk respon kosong — jangan di-prune
		// agar urutan tool-call/tool-result tidak halusinasi.
		out = append(out, as)
		for _, s := range group {
			toolMsg := &messages.Message{Role: "tool"}
			messages.SetContentParts(toolMsg, []messages.Part{{
				"type":       mustJSON("tool-result"),
				"toolCallId": mustJSON(s.Action.ToolID),
				"toolName":   mustJSON(s.Action.Tool),
				"output":     mustJSON(map[string]any{"type": "json", "value": parseObservation(s.Observation)}),
			}})
			out = append(out, toolMsg)
		}
		i = j
	}
	return out
}

func parseObservation(s string) any {
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		return v
	}
	return s
}

func reasoningForStep(reasoningByStep []string, idx int) string {
	if idx < 0 || idx >= len(reasoningByStep) {
		return ""
	}
	return reasoningByStep[idx]
}

func stepsFromValues(vals map[string]any) []schema.AgentStep {
	if raw, ok := vals["intermediateSteps"].([]schema.AgentStep); ok {
		return raw
	}
	return nil
}

func outputFromValues(vals map[string]any) string {
	s, _ := vals["output"].(string)
	return s
}

// langTool adapts a *Tool to langchaingo tools.Tool.
type langTool struct {
	name string
	desc string
	run  func(ctx context.Context, args map[string]any) (any, error)
}

func (t *langTool) Name() string        { return t.name }
func (t *langTool) Description() string { return t.desc }

func (t *langTool) Call(ctx context.Context, input string) (string, error) {
	args := map[string]any{}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		args["input"] = strings.TrimSpace(input)
	}
	val, runErr := t.run(ctx, args)
	if runErr != nil {
		val = map[string]any{"error": runErr.Error()}
	}
	b, err := json.Marshal(val)
	if err != nil {
		return `{"error":"failed to serialize tool output"}`, nil
	}
	return string(b), nil
}

func wrapTools(toolMap map[string]*Tool) []tools.Tool {
	out := make([]tools.Tool, 0, len(toolMap))
	for _, t := range toolMap {
		out = append(out, &langTool{name: t.Name, desc: t.Description, run: t.Run})
	}
	return out
}

// toFunctionDefinitions converts tools to OpenAI function definitions, ensuring
// every Parameters schema is a valid JSON object (strict OpenAI-compatible
// providers reject a null properties / parameters).
func toFunctionDefinitions(toolMap map[string]*Tool) []llms.FunctionDefinition {
	out := make([]llms.FunctionDefinition, 0, len(toolMap))
	for _, t := range toolMap {
		out = append(out, llms.FunctionDefinition{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  sanitizeParams(t.Parameters),
		})
	}
	return out
}

func sanitizeParams(p map[string]any) map[string]any {
	if p == nil {
		p = map[string]any{}
	}
	props, _ := p["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
		p["properties"] = props
	}
	p["type"] = "object"
	return p
}

// requestAgent is the langchain Agent for one request.
type requestAgent struct {
	llm          llms.Model
	system       string
	history      []llms.ChatMessage
	tools        []tools.Tool
	functions    []llms.FunctionDefinition
	temperature  float64
	chatTemplate prompts.ChatPromptTemplate
	loopDelay    time.Duration
	plans        int

	totalTokens      int
	lastUsage        Usage
	lastFinishReason string
	nextToolIDSeq    int
	reasoningByStep  []string
	lastReasoning    string
}

func newRequestAgent(model llms.Model, system string, history []*messages.Message, toolMap map[string]*Tool, temperature float64, loopDelay time.Duration) *requestAgent {
	conv := toChatHistory(history)
	if conv == nil {
		conv = []llms.ChatMessage{}
	}
	return &requestAgent{
		llm:         model,
		system:      system,
		tools:       wrapTools(toolMap),
		functions:   toFunctionDefinitions(toolMap),
		temperature: temperature,
		history:     conv,
		loopDelay:   loopDelay,
		chatTemplate: prompts.NewChatPromptTemplate([]prompts.MessageFormatter{
			systemMessageFormatter(system),
			prompts.MessagesPlaceholder{VariableName: "chat_history"},
			prompts.NewHumanMessagePromptTemplate("{{.input}}", []string{"input"}),
			prompts.MessagesPlaceholder{VariableName: "agent_scratchpad"},
		}),
	}
}

func (ra *requestAgent) Plan(
	ctx context.Context,
	intermediateSteps []schema.AgentStep,
	inputs map[string]string,
	options ...chains.ChainCallOption,
) ([]schema.AgentAction, *schema.AgentFinish, error) {
	// Jeda antar loop (bukan sebelum iterasi pertama); hormat ctx cancel.
	if ra.plans > 0 && ra.loopDelay > 0 {
		sleepCtx(ctx, ra.loopDelay)
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
	}
	ra.plans++
	pv, err := ra.chatTemplate.FormatPrompt(map[string]any{
		"input":            inputs["input"],
		"chat_history":     ra.history,
		"agent_scratchpad": stepsToChatMessages(intermediateSteps, ra.reasoningByStep),
	})
	if err != nil {
		return nil, nil, err
	}

	formatted := pv.Messages()
	msgList := make([]llms.MessageContent, 0, len(formatted))
	reasonings := make([]string, 0, len(formatted))
	for _, m := range formatted {
		reasoning := ""
		if am, ok := m.(llms.AIChatMessage); ok {
			reasoning = am.ReasoningContent
		}
		msgList = append(msgList, chatMessageToContent(m))
		reasonings = append(reasonings, reasoning)
	}

	callOpts := []llms.CallOption{llms.WithStreamingFunc(noopStream)}
	if ra.temperature != 0 {
		callOpts = append(callOpts, llms.WithTemperature(ra.temperature))
	}
	if len(ra.functions) > 0 {
		callOpts = append(callOpts, llms.WithFunctions(ra.functions))
	}

	var resp *llms.ContentResponse
	if rc, ok := ra.llm.(reasoningClient); ok {
		resp, err = rc.GenerateContentWithReasoning(ctx, msgList, reasonings, callOpts...)
	} else {
		resp, err = ra.llm.GenerateContent(ctx, msgList, callOpts...)
	}
	if err != nil {
		log.Printf("[ai] GenerateContent failed: %v", err)
		return nil, nil, err
	}
	if resp == nil || len(resp.Choices) == 0 {
		log.Printf("[ai] model returned empty response")
		return nil, nil, errors.New("empty model response")
	}
	ra.totalTokens += usageFromChoices(resp)
	ra.lastUsage = lastUsageFrom(resp)

	choice := resp.Choices[0]
	ra.lastFinishReason = choice.StopReason
	if len(choice.ToolCalls) > 0 {
		actions := make([]schema.AgentAction, 0, len(choice.ToolCalls))
		for _, tc := range choice.ToolCalls {
			actions = append(actions, schema.AgentAction{
				Tool:      tc.FunctionCall.Name,
				ToolInput: toolArgsJSON(tc.FunctionCall.Arguments),
				ToolID:    ra.toolID(tc.ID),
				Log:       choice.Content,
			})
		}
		ra.recordReasoning(choice.ReasoningContent, len(intermediateSteps), len(actions))
		return actions, nil, nil
	}
	if choice.FuncCall != nil {
		ra.recordReasoning(choice.ReasoningContent, len(intermediateSteps), 1)
		return []schema.AgentAction{{
			Tool:      choice.FuncCall.Name,
			ToolInput: toolArgsJSON(choice.FuncCall.Arguments),
			ToolID:    ra.toolID(""),
			Log:       choice.Content,
		}}, nil, nil
	}
	ra.lastReasoning = choice.ReasoningContent
	return nil, &schema.AgentFinish{
		ReturnValues: map[string]any{"output": choice.Content},
		Log:          choice.Content,
	}, nil
}

func (ra *requestAgent) recordReasoning(reasoning string, startIdx, nSteps int) {
	if nSteps <= 0 {
		return
	}
	for k := 0; k < nSteps; k++ {
		idx := startIdx + k
		for idx >= len(ra.reasoningByStep) {
			ra.reasoningByStep = append(ra.reasoningByStep, "")
		}
		ra.reasoningByStep[idx] = reasoning
	}
}

func usageFromChoices(resp *llms.ContentResponse) int {
	if resp == nil || len(resp.Choices) == 0 {
		return 0
	}
	return asInt(resp.Choices[0].GenerationInfo["TotalTokens"])
}

func lastUsageFrom(resp *llms.ContentResponse) Usage {
	if resp == nil || len(resp.Choices) == 0 {
		return Usage{}
	}
	return tokenUsageInfo(resp.Choices[0].GenerationInfo)
}

func tokenUsageInfo(gi map[string]any) Usage {
	return Usage{
		InputTokens:  asInt(gi["PromptTokens"]),
		OutputTokens: asInt(gi["CompletionTokens"]),
		TotalTokens:  asInt(gi["TotalTokens"]),
	}
}

func (ra *requestAgent) GetInputKeys() []string  { return []string{"input"} }
func (ra *requestAgent) GetOutputKeys() []string { return []string{"output"} }
func (ra *requestAgent) GetTools() []tools.Tool  { return ra.tools }

func (ra *requestAgent) toolID(id string) string {
	if strings.TrimSpace(id) != "" {
		return id
	}
	ra.nextToolIDSeq++
	return fmt.Sprintf("call_%d", ra.nextToolIDSeq)
}

func (a *Agent) maxSteps() int {
	if a.Config != nil && a.Config.MaxIterations > 0 {
		return a.Config.MaxIterations
	}
	return config.DefaultMaxIterations
}

func (a *Agent) temperature() float64 {
	if a.Config != nil {
		return a.Config.Model.Temperature
	}
	return 0
}

func (a *Agent) loopDelay() time.Duration {
	if a.Config != nil {
		return a.Config.LoopDelay()
	}
	return config.DefaultLoopDelaySeconds * time.Second
}

// sleepCtx sleeps d or until ctx is done.
func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func (a *Agent) toolsFor(opts *ProcessOptions) (map[string]*Tool, error) {
	if a.ToolsBuild != nil {
		return a.ToolsBuild(opts)
	}
	return BuildTools(a, opts), nil
}

func (a *Agent) runOnce(ctx context.Context, system string, history []*messages.Message, userText string, opts *ProcessOptions, toolMap map[string]*Tool) (*runResult, error) {
	if a.Client == nil {
		return nil, errNoModel
	}
	maxSteps := a.maxSteps()
	ra := newRequestAgent(a.Client, system, history, toolMap, a.temperature(), a.loopDelay())

	executor := agents.NewExecutor(ra,
		agents.WithMaxIterations(maxSteps),
		agents.WithReturnIntermediateSteps(),
	)

	vals, runErr := executor.Call(ctx, map[string]any{"input": userText})

	res := &runResult{
		totalTokens:      ra.totalTokens,
		lastStepUsage:    ra.lastUsage,
		lastFinishReason: ra.lastFinishReason,
	}
	steps := stepsFromValues(vals)
	if len(steps) > maxSteps {
		res.hitStepLimit = true
	}
	res.responseMessages = responseFromSteps(steps, ra.reasoningByStep)

	if errors.Is(runErr, agents.ErrNotFinished) {
		res.hitStepLimit = true
		return res, nil
	}
	if runErr != nil {
		return nil, runErr
	}

	res.finalText = strings.TrimSpace(outputFromValues(vals))
	if res.finalText != "" {
		m := &messages.Message{Role: "assistant"}
		if reasoning := strings.TrimSpace(ra.lastReasoning); reasoning != "" {
			messages.SetContentParts(m, []messages.Part{
				{"type": mustJSON("reasoning"), "text": mustJSON(reasoning)},
				{"type": mustJSON("text"), "text": mustJSON(res.finalText)},
			})
		} else {
			messages.SetContentString(m, res.finalText)
		}
		res.responseMessages = append(res.responseMessages, m)
	}
	return res, nil
}

// ProcessMessage runs one request: NO history trimming here — the caller
// (app layer) summarizes history with the model into a context/*.md file when
// the token limit is hit, wipes history, and the newest summary is injected
// into the system prompt (see memory.LatestSummary).
// Single executor run, no provider fallback; API errors are retried per model
// call (5x total, 2s delay) inside the model wrapper.
func (a *Agent) ProcessMessage(ctx context.Context, userMessage string, history []*messages.Message, opts *ProcessOptions) *ProcessResult {
	memoryContent := ""
	summary := ""
	if a.Config != nil {
		if b, err := os.ReadFile(a.Config.MemoryPath()); err == nil {
			memoryContent = string(b)
		}
		summary = memory.LatestSummary(a.Config.Workspace)
	}

	systemPrompt, err := prompt.Get(memoryContent, summary)
	if err != nil {
		log.Printf("[ai] prompt.Get failed: %v", err)
		systemPrompt = ""
	}

	toolMap, terr := a.toolsFor(opts)
	if terr != nil || len(toolMap) == 0 {
		log.Printf("[ai] ToolsBuild failed (err=%v, tools=%d)", terr, len(toolMap))
		return errResult()
	}

	run, rerr := a.runOnce(ctx, systemPrompt, history, userMessage, opts, toolMap)
	if rerr != nil {
		log.Printf("[ai] run failed: %v", rerr)
		return errResult()
	}
	if run.hitStepLimit {
		return makeResult(stepLimitHint, run.responseMessages, run.totalTokens, run.lastStepUsage, run.lastFinishReason)
	}
	if strings.TrimSpace(run.finalText) != "" {
		return makeResult(run.finalText, run.responseMessages, run.totalTokens, run.lastStepUsage, run.lastFinishReason)
	}
	log.Printf("[ai] empty final text (finish_reason=%q)", run.lastFinishReason)
	return errResult()
}

func makeResult(text string, resp []*messages.Message, total int, usage Usage, finishReason string) *ProcessResult {
	if strings.TrimSpace(text) == "" {
		text = "Sorry, I can't respond right now."
	}
	return &ProcessResult{Text: text, ResponseMessages: resp, TotalTokens: total, LastStepUsage: usage, LastFinishReason: finishReason}
}

func errResult() *ProcessResult {
	return &ProcessResult{Text: "Sorry, I can't respond right now."}
}
