// Package ai: single OpenAI-compatible model from config.json.
package ai

import (
	"context"
	"net/http"

	"github.com/tmc/langchaingo/llms"

	"github.com/purujawa06-bot/PURU-AI/internal/ai/openai"
	"github.com/purujawa06-bot/PURU-AI/internal/config"
)

// noopStream forces stream:true without processing chunks.
func noopStream(context.Context, []byte) error { return nil }

// NewModel builds the single model from config (no fallback, no relay).
func NewModel(cfg *config.Config, hc *http.Client) (llms.Model, error) {
	return openai.New(cfg.Model.BaseURL, cfg.Model.APIKey, cfg.Model.Model, hc)
}
