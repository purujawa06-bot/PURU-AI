package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/tmc/langchaingo/llms"
)

// flakyModel fails failLeft times, then returns an empty (but valid) response.
type flakyModel struct {
	failLeft int
	calls    int
	streamed bool
}

func (f *flakyModel) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	f.calls++
	for _, opt := range options {
		var o llms.CallOptions
		opt(&o)
		if o.StreamingFunc != nil {
			f.streamed = true
		}
	}
	if f.failLeft > 0 {
		f.failLeft--
		return "", errors.New("API returned unexpected status code: 503")
	}
	return "ok", nil
}

func (f *flakyModel) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	if _, err := f.Call(ctx, "", options...); err != nil {
		return nil, err
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "ok"}}}, nil
}

func TestRetrySucceedsAfterFlakes(t *testing.T) {
	inner := &flakyModel{failLeft: 2}
	r := &retryModel{inner: inner}
	out, err := r.Call(context.Background(), "hi")
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if out != "ok" || inner.calls != 3 {
		t.Fatalf("out=%q calls=%d, want ok/3", out, inner.calls)
	}
}

func TestRetryGivesUpAfterFive(t *testing.T) {
	inner := &flakyModel{failLeft: 99}
	r := &retryModel{inner: inner}
	if _, err := r.Call(context.Background(), "hi"); err == nil {
		t.Fatalf("expected error after exhausting retries")
	}
	if inner.calls != maxAPIAttempts {
		t.Fatalf("calls=%d, want %d", inner.calls, maxAPIAttempts)
	}
}

func TestRetryCancelledContext(t *testing.T) {
	inner := &flakyModel{failLeft: 99}
	r := &retryModel{inner: inner}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Call(ctx, "hi"); err == nil {
		t.Fatalf("expected context error, retries must not run")
	}
	if inner.calls != 0 {
		t.Fatalf("calls=%d, want 0 (no attempt on cancelled ctx)", inner.calls)
	}
}
