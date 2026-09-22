package memory

import "errors"

var (
	errNoModel      = errors.New("no model configured for summarization")
	errEmptySummary = errors.New("model returned empty summary")
)
