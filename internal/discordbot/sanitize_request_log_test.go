package discordbot

import "testing"

func TestHeadersForLog_stripsSignatureAndAuth(t *testing.T) {
	t.Parallel()
	in := map[string]string{
		"Content-Type":          "application/json",
		"x-signature-ed25519":   "deadbeef",
		"X-Signature-Timestamp": "1234567890",
		"Authorization":         "Bearer secret",
		"User-Agent":            "Discordbot",
	}
	got := headersForLog(in)
	if got["Content-Type"] != "application/json" {
		t.Fatalf("Content-Type: %+v", got)
	}
	if got["User-Agent"] != "Discordbot" {
		t.Fatalf("User-Agent: %+v", got)
	}
	for _, k := range []string{"x-signature-ed25519", "X-Signature-Timestamp", "Authorization"} {
		if _, ok := got[k]; ok {
			t.Fatalf("expected %q stripped", k)
		}
	}
}

func TestQueryStringParamsForLog_stripsSecrets(t *testing.T) {
	t.Parallel()
	in := map[string]string{
		"foo":          "bar",
		"access_token": "secret",
		"CODE":         "abc",
	}
	got := queryStringParamsForLog(in)
	if got["foo"] != "bar" {
		t.Fatalf("foo: %+v", got)
	}
	if len(got) != 1 {
		t.Fatalf("expected only foo, got %+v", got)
	}
}
