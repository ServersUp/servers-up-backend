package feedback

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

// fakeMailer records the last submission and returns a configurable error.
type fakeMailer struct {
	lastSubmission Submission
	sendErr        error
}

func (f *fakeMailer) Send(_ context.Context, s Submission) error {
	f.lastSubmission = s
	return f.sendErr
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestHandleRequest(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		origin     string
		body       string
		b64Encoded bool
		sendErr    error
		wantStatus int
		wantCORS   bool
		wantBody   string
		// verifySubmission, when non-nil, checks the submission passed to the mailer.
		verifySubmission func(t *testing.T, s Submission)
	}{
		{
			name:       "OPTIONS allowed origin",
			method:     http.MethodOptions,
			origin:     allowedOrigin,
			wantStatus: http.StatusNoContent,
			wantCORS:   true,
			wantBody:   "",
		},
		{
			name:       "OPTIONS wrong origin",
			method:     http.MethodOptions,
			origin:     "https://evil.example.com",
			wantStatus: http.StatusForbidden,
			wantCORS:   false,
			wantBody:   `{"error":"request failed"}`,
		},
		{
			name:       "POST success",
			method:     http.MethodPost,
			origin:     allowedOrigin,
			body:       `{"message":"hello world"}`,
			wantStatus: http.StatusOK,
			wantCORS:   true,
			wantBody:   `{"status":"accepted"}`,
			verifySubmission: func(t *testing.T, s Submission) {
				if s.Kind != KindFeedback {
					t.Errorf("Kind = %q, want %q", s.Kind, KindFeedback)
				}
				if s.Source != SourceWebsite {
					t.Errorf("Source = %q, want %q", s.Source, SourceWebsite)
				}
				if s.Message != "hello world" {
					t.Errorf("Message = %q, want %q", s.Message, "hello world")
				}
			},
		},
		{
			name:       "POST with subscriptionId",
			method:     http.MethodPost,
			origin:     allowedOrigin,
			body:       `{"message":"hi","subscriptionId":"sub-123"}`,
			wantStatus: http.StatusOK,
			wantCORS:   true,
			wantBody:   `{"status":"accepted"}`,
			verifySubmission: func(t *testing.T, s Submission) {
				if s.SubscriptionID != "sub-123" {
					t.Errorf("SubscriptionID = %q, want %q", s.SubscriptionID, "sub-123")
				}
			},
		},
		{
			name:       "POST wrong origin",
			method:     http.MethodPost,
			origin:     "https://evil.example.com",
			body:       `{"message":"hello"}`,
			wantStatus: http.StatusForbidden,
			wantCORS:   false,
			wantBody:   `{"error":"request failed"}`,
		},
		{
			name:       "POST missing origin",
			method:     http.MethodPost,
			origin:     "",
			body:       `{"message":"hello"}`,
			wantStatus: http.StatusForbidden,
			wantCORS:   false,
			wantBody:   `{"error":"request failed"}`,
		},
		{
			name:       "GET rejected",
			method:     http.MethodGet,
			origin:     allowedOrigin,
			wantStatus: http.StatusMethodNotAllowed,
			wantCORS:   true,
			wantBody:   `{"error":"request failed"}`,
		},
		{
			name:       "PUT rejected",
			method:     http.MethodPut,
			origin:     allowedOrigin,
			wantStatus: http.StatusMethodNotAllowed,
			wantCORS:   true,
			wantBody:   `{"error":"request failed"}`,
		},
		{
			name:       "valid base64-encoded body",
			method:     http.MethodPost,
			origin:     allowedOrigin,
			body:       b64(`{"message":"encoded hello"}`),
			b64Encoded: true,
			wantStatus: http.StatusOK,
			wantCORS:   true,
			wantBody:   `{"status":"accepted"}`,
			verifySubmission: func(t *testing.T, s Submission) {
				if s.Message != "encoded hello" {
					t.Errorf("Message = %q, want %q", s.Message, "encoded hello")
				}
			},
		},
		{
			name:       "malformed base64",
			method:     http.MethodPost,
			origin:     allowedOrigin,
			body:       "not-valid-base64!!!",
			b64Encoded: true,
			wantStatus: http.StatusBadRequest,
			wantCORS:   true,
			wantBody:   `{"error":"request failed"}`,
		},
		{
			name:       "malformed JSON",
			method:     http.MethodPost,
			origin:     allowedOrigin,
			body:       `{not json}`,
			wantStatus: http.StatusBadRequest,
			wantCORS:   true,
			wantBody:   `{"error":"request failed"}`,
		},
		{
			name:       "trailing JSON",
			method:     http.MethodPost,
			origin:     allowedOrigin,
			body:       `{"message":"hi"} extra`,
			wantStatus: http.StatusBadRequest,
			wantCORS:   true,
			wantBody:   `{"error":"request failed"}`,
		},
		{
			name:       "empty message validation failure",
			method:     http.MethodPost,
			origin:     allowedOrigin,
			body:       `{"message":"   "}`,
			wantStatus: http.StatusBadRequest,
			wantCORS:   true,
			wantBody:   `{"error":"request failed"}`,
		},
		{
			name:       "message too long validation failure",
			method:     http.MethodPost,
			origin:     allowedOrigin,
			body:       `{"message":"` + string(make([]byte, MaxMessageLength+1)) + `"}`,
			wantStatus: http.StatusBadRequest,
			wantCORS:   true,
			wantBody:   `{"error":"request failed"}`,
		},
		{
			name:       "delivery error returns generic 500",
			method:     http.MethodPost,
			origin:     allowedOrigin,
			body:       `{"message":"hello"}`,
			sendErr:    errors.New("SES provider: throttled"),
			wantStatus: http.StatusInternalServerError,
			wantCORS:   true,
			wantBody:   `{"error":"request failed"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm := &fakeMailer{sendErr: tt.sendErr}
			h := NewHTTPHandler(fm)

			headers := map[string]string{}
			if tt.origin != "" {
				headers["Origin"] = tt.origin
			}

			req := events.LambdaFunctionURLRequest{
				RequestContext: events.LambdaFunctionURLRequestContext{
					HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{
						Method: tt.method,
					},
				},
				Headers:         headers,
				Body:            tt.body,
				IsBase64Encoded: tt.b64Encoded,
			}

			resp, err := h.HandleRequest(context.Background(), req)
			if err != nil {
				t.Fatalf("HandleRequest returned error: %v", err)
			}

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			if tt.wantCORS {
				if got := resp.Headers["Access-Control-Allow-Origin"]; got != allowedOrigin {
					t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, allowedOrigin)
				}
			} else {
				if got := resp.Headers["Access-Control-Allow-Origin"]; got != "" {
					t.Errorf("Access-Control-Allow-Origin = %q, want empty", got)
				}
			}

			if resp.Body != tt.wantBody {
				t.Errorf("Body = %q, want %q", resp.Body, tt.wantBody)
			}

			// Assert no provider error or message body leakage in the response.
			if tt.sendErr != nil {
				if contains(resp.Body, tt.sendErr.Error()) {
					t.Errorf("response body leaks provider error: %q", resp.Body)
				}
			}
			if tt.name != "POST success" && tt.name != "POST with subscriptionId" && tt.name != "valid base64-encoded body" {
				// Non-success responses must not echo the message.
				if tt.body != "" {
					decoded := tt.body
					if tt.b64Encoded {
						if d, err := base64.StdEncoding.DecodeString(tt.body); err == nil {
							decoded = string(d)
						}
					}
					if len(decoded) > 0 && contains(resp.Body, decoded) {
						t.Errorf("response body leaks request body: %q", resp.Body)
					}
				}
			}

			if tt.verifySubmission != nil {
				tt.verifySubmission(t, fm.lastSubmission)
			}
		})
	}
}

func TestHandleRequestLowercaseOriginHeader(t *testing.T) {
	fm := &fakeMailer{}
	h := NewHTTPHandler(fm)

	req := events.LambdaFunctionURLRequest{
		RequestContext: events.LambdaFunctionURLRequestContext{
			HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{
				Method: http.MethodPost,
			},
		},
		Headers: map[string]string{
			"origin": allowedOrigin,
		},
		Body: `{"message":"lowercase origin"}`,
	}

	resp, err := h.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest returned error: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Headers["Access-Control-Allow-Origin"]; got != allowedOrigin {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, allowedOrigin)
	}
	if resp.Body != `{"status":"accepted"}` {
		t.Errorf("Body = %q, want %q", resp.Body, `{"status":"accepted"}`)
	}

	if fm.lastSubmission.Kind != KindFeedback {
		t.Errorf("Kind = %q, want %q", fm.lastSubmission.Kind, KindFeedback)
	}
	if fm.lastSubmission.Source != SourceWebsite {
		t.Errorf("Source = %q, want %q", fm.lastSubmission.Source, SourceWebsite)
	}
	if fm.lastSubmission.Message != "lowercase origin" {
		t.Errorf("Message = %q, want %q", fm.lastSubmission.Message, "lowercase origin")
	}
}

// contains reports whether s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
