package discord

import (
	"errors"
	"fmt"
)

// APIError is a non-2xx response from the Discord REST API.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	if e == nil {
		return "discord: api error"
	}
	return fmt.Sprintf("discord: api error status=%d body=%q", e.StatusCode, truncateAPIErrorBody(e.Body))
}

func truncateAPIErrorBody(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// Permanent reports client errors that should not be retried via SQS (ack-delete on primary).
// All 4xx except 429 (rate limit) are permanent.
func (e *APIError) Permanent() bool {
	if e == nil {
		return false
	}
	return e.StatusCode >= 400 && e.StatusCode < 500 && e.StatusCode != 429
}

// Retryable reports errors that should return BatchItemFailure (429 rate limit, 5xx).
func (e *APIError) Retryable() bool {
	if e == nil {
		return false
	}
	return e.StatusCode == 429 || (e.StatusCode >= 500 && e.StatusCode < 600)
}

// AsAPIError returns the Discord APIError if err wraps one.
func AsAPIError(err error) (*APIError, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}
