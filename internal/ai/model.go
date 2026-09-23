// Package ai: single OpenAI-compatible model from config.json.
package ai

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/tmc/langchaingo/llms"

	"github.com/purujawa06-bot/PURU-AI/internal/ai/openai"
	"github.com/purujawa06-bot/PURU-AI/internal/config"
)

// noopStream forces stream:true without processing chunks.
func noopStream(context.Context, []byte) error { return nil }

const (
	// maxAPIAttempts: tiap pemanggilan API yang error diulang sampai total 5x.
	maxAPIAttempts = 5
	// apiRetryDelay: jeda antar percobaan ulang.
	apiRetryDelay = 2 * time.Second
)

// retryModel wraps an llms.Model: every GenerateContent/Call that returns an
// API error is retried up to maxAPIAttempts total with apiRetryDelay between
// attempts. Retries happen per model call (inside one executor step), so
// already-executed tools are never re-run.
type retryModel struct {
	inner llms.Model
}

// sleepOrDone waits d or returns false when ctx is cancelled first.
func sleepOrDone(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// do runs fn with retries, logging each failed attempt.
func (r *retryModel) do(ctx context.Context, what string, fn func() error) error {
	var err error
	for attempt := 1; attempt <= maxAPIAttempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = fn(); err == nil {
			return nil
		}
		if attempt == maxAPIAttempts {
			break
		}
		log.Printf("[ai] API error (%s %d/%d): %v — retrying in %v", what, attempt, maxAPIAttempts, err, apiRetryDelay)
		if !sleepOrDone(ctx, apiRetryDelay) {
			return ctx.Err()
		}
	}
	return err
}

// Call is the llms.Model convenience wrapper, with retries.
func (r *retryModel) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	var out string
	err := r.do(ctx, "Call", func() error {
		var e error
		out, e = r.inner.Call(ctx, prompt, options...)
		return e
	})
	return out, err
}

// GenerateContent implements llms.Model, with retries.
func (r *retryModel) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	var out *llms.ContentResponse
	err := r.do(ctx, "GenerateContent", func() error {
		var e error
		out, e = r.inner.GenerateContent(ctx, messages, options...)
		return e
	})
	return out, err
}

// GenerateContentWithReasoning mirrors reasoningClient, with retries — kept so
// the agent's type assertion still selects the reasoning path (thinking-mode
// providers need reasoning_content echoed back).
func (r *retryModel) GenerateContentWithReasoning(ctx context.Context, messages []llms.MessageContent, reasonings []string, options ...llms.CallOption) (*llms.ContentResponse, error) {
	rc, ok := r.inner.(reasoningClient)
	if !ok {
		return r.GenerateContent(ctx, messages, options...)
	}
	var out *llms.ContentResponse
	err := r.do(ctx, "GenerateContent", func() error {
		var e error
		out, e = rc.GenerateContentWithReasoning(ctx, messages, reasonings, options...)
		return e
	})
	return out, err
}

// NewModel builds the single model from config (no fallback, no relay).
// Every API call streams (agent Plan always sets a StreamingFunc)
// and is retried up to 5x total on API errors.
func NewModel(cfg *config.Config, hc *http.Client) (llms.Model, error) {
	inner, err := openai.New(cfg.Model.BaseURL, cfg.Model.APIKey, cfg.Model.Model, hc)
	if err != nil {
		return nil, err
	}
	return &retryModel{inner: inner}, nil
}
