package discord

import (
	"encoding/json"
	"errors"
	"fmt"
)

// APIError is a non-2xx response from the Discord REST API.
type APIError struct {
	StatusCode int
	Body       string
	errorCode  int
}

func (e *APIError) Error() string {
	if e == nil {
		return "discord: api error"
	}
	return fmt.Sprintf("discord: api error status=%d body=%q", e.StatusCode, truncateAPIErrorBody(e.Body))
}

// ErrorCode returns the Discord error code from the response body (e.g. 50007
// for "the user has their DM settings set to private"), or 0 if absent.
func (e *APIError) ErrorCode() int {
	if e == nil {
		return 0
	}
	return e.errorCode
}

func truncateAPIErrorBody(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// ErrorCodeFromBody extracts the Discord "code" from an error body, or 0 if
// the body is not a Discord error envelope.
func ErrorCodeFromBody(body string) int {
	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return 0
	}
	return envelope.Code
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

// NewAPIError builds an APIError from a non-2xx status and raw response body,
// parsing the Discord error code if the body is a Discord error envelope.
func NewAPIError(statusCode int, body string) *APIError {
	return &APIError{StatusCode: statusCode, Body: body, errorCode: ErrorCodeFromBody(body)}
}

// AsAPIError returns the Discord APIError if err wraps one.
func AsAPIError(err error) (*APIError, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}
