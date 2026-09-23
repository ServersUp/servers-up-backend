package feedback

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
)

const (
	// allowedOrigin is the only origin permitted to submit feedback.
	allowedOrigin = "https://serversup.armasn.dev"

	// maxBodyBytes bounds the decoded request body.
	maxBodyBytes = 64 * 1024
)

// HTTPHandler serves the website feedback endpoint on the feedback Lambda.
type HTTPHandler struct {
	mailer Mailer
}

// NewHTTPHandler returns an HTTPHandler that delivers submissions via mailer.
func NewHTTPHandler(mailer Mailer) *HTTPHandler {
	return &HTTPHandler{mailer: mailer}
}

// websiteRequest is the JSON payload accepted from the website.
type websiteRequest struct {
	Message        string `json:"message"`
	SubscriptionID string `json:"subscriptionId,omitempty"`
}

// HandleRequest processes a Lambda Function URL request event.
func (h *HTTPHandler) HandleRequest(ctx context.Context, req events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	origin := req.Headers["Origin"]
	if origin == "" {
		origin = req.Headers["origin"]
	}

	// CORS preflight: only for the allowed origin.
	if req.RequestContext.HTTP.Method == http.MethodOptions {
		if origin != allowedOrigin {
			return h.corsResponse(http.StatusForbidden, false), nil
		}
		return h.corsResponse(http.StatusNoContent, true), nil
	}

	// Only POST is accepted for actual submissions.
	if req.RequestContext.HTTP.Method != http.MethodPost {
		return h.corsResponse(http.StatusMethodNotAllowed, origin == allowedOrigin), nil
	}

	// Origin must match exactly.
	if origin != allowedOrigin {
		return h.corsResponse(http.StatusForbidden, false), nil
	}

	// Decode the body: base64 only when the event is flagged as encoded.
	var body []byte
	if req.IsBase64Encoded {
		var err error
		body, err = base64.StdEncoding.DecodeString(req.Body)
		if err != nil {
			slog.Warn("feedback: body decode failed", "error", err)
			return h.corsResponse(http.StatusBadRequest, true), nil
		}
	} else {
		body = []byte(req.Body)
	}
	if len(body) > maxBodyBytes {
		slog.Warn("feedback: body too large", "size", len(body))
		return h.corsResponse(http.StatusBadRequest, true), nil
	}

	// Decode JSON strictly (reject trailing data).
	var payload websiteRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&payload); err != nil {
		slog.Warn("feedback: JSON decode failed", "error", err)
		return h.corsResponse(http.StatusBadRequest, true), nil
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		slog.Warn("feedback: trailing JSON data")
		return h.corsResponse(http.StatusBadRequest, true), nil
	}

	// Force server-side kind and source; never trust client values.
	submission := Submission{
		Kind:           KindFeedback,
		Source:         SourceWebsite,
		Message:        payload.Message,
		SubscriptionID: payload.SubscriptionID,
	}

	if err := submission.Validate(); err != nil {
		slog.Warn("feedback: validation failed", "error", err)
		return h.corsResponse(http.StatusBadRequest, true), nil
	}

	if err := h.mailer.Send(ctx, submission); err != nil {
		slog.Error("feedback: delivery failed", "error", err)
		return h.corsResponse(http.StatusInternalServerError, true), nil
	}

	slog.Info("feedback: accepted")
	return h.corsResponse(http.StatusOK, true), nil
}

// corsResponse builds a JSON response with fixed CORS headers for the allowed origin.
func (h *HTTPHandler) corsResponse(status int, allowed bool) events.LambdaFunctionURLResponse {
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	if allowed {
		headers["Access-Control-Allow-Origin"] = allowedOrigin
		headers["Access-Control-Allow-Methods"] = "POST, OPTIONS"
		headers["Access-Control-Allow-Headers"] = "Content-Type"
	}

	var body string
	switch status {
	case http.StatusNoContent:
		body = ""
	case http.StatusOK:
		body = `{"status":"accepted"}`
	default:
		body = `{"error":"request failed"}`
	}

	return events.LambdaFunctionURLResponse{
		StatusCode: status,
		Headers:    headers,
		Body:       body,
	}
}
