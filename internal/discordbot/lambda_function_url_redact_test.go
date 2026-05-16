package discordbot

import (
	"log/slog"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

func headersAttrValue(attrs []slog.Attr) (map[string]string, bool) {
	for _, a := range attrs {
		if a.Key != "headers" {
			continue
		}
		v := a.Value.Any()
		m, ok := v.(map[string]string)
		return m, ok
	}
	return nil, false
}

func TestLambdaFunctionURLDebugAttrs_omitsSensitiveHeaders(t *testing.T) {
	t.Parallel()
	req := events.LambdaFunctionURLRequest{
		Version: "2.0",
		Headers: map[string]string{
			"Content-Type":          "application/json",
			"x-signature-ed25519":   "deadbeef",
			"X-Signature-Timestamp": "1234567890",
			"Authorization":         "Bearer x",
			"User-Agent":            "DiscordBot",
		},
		RequestContext: events.LambdaFunctionURLRequestContext{
			RequestID: "rid",
			HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{
				Method: "POST",
				Path:   "/",
			},
		},
	}
	attrs := LambdaFunctionURLDebugAttrs(req, 10)
	m, ok := headersAttrValue(attrs)
	if !ok {
		t.Fatal("expected headers attr")
	}
	if m["Content-Type"] != "application/json" || m["User-Agent"] != "DiscordBot" {
		t.Fatalf("unexpected headers map: %+v", m)
	}
	for _, k := range []string{"x-signature-ed25519", "X-Signature-Timestamp", "Authorization"} {
		if _, exists := m[k]; exists {
			t.Fatalf("expected %q stripped from logged headers", k)
		}
	}
}

func TestRedactQueryStringParamsForLog_stripsSecrets(t *testing.T) {
	t.Parallel()
	in := map[string]string{
		"foo":          "bar",
		"access_token": "secret",
		"CODE":         "abc",
	}
	got := redactQueryStringParamsForLog(in)
	if got["foo"] != "bar" {
		t.Fatalf("foo: %+v", got)
	}
	if len(got) != 1 {
		t.Fatalf("expected only foo, got %+v", got)
	}
}
