package discord

import (
	"errors"
	"fmt"
	"testing"
)

func TestAPIError_Permanent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code int
		want bool
	}{
		{399, false},
		{400, true},
		{401, true},
		{403, true},
		{404, true},
		{429, false},
		{499, true},
		{500, false},
		{503, false},
	}
	for _, tc := range cases {
		e := &APIError{StatusCode: tc.code}
		if got := e.Permanent(); got != tc.want {
			t.Errorf("Permanent(%d) = %v want %v", tc.code, got, tc.want)
		}
	}
	if (*APIError)(nil).Permanent() {
		t.Fatal("nil Permanent should be false")
	}
}

func TestAPIError_Retryable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code int
		want bool
	}{
		{403, false},
		{429, true},
		{499, false},
		{500, true},
		{503, true},
		{599, true},
		{600, false},
	}
	for _, tc := range cases {
		e := &APIError{StatusCode: tc.code}
		if got := e.Retryable(); got != tc.want {
			t.Errorf("Retryable(%d) = %v want %v", tc.code, got, tc.want)
		}
	}
}

func TestAsAPIError(t *testing.T) {
	t.Parallel()
	inner := &APIError{StatusCode: 403, Body: "forbidden"}
	wrapped := fmt.Errorf("send: %w", inner)
	got, ok := AsAPIError(wrapped)
	if !ok || got.StatusCode != 403 {
		t.Fatalf("AsAPIError: got %+v ok=%v", got, ok)
	}
	if _, ok := AsAPIError(errors.New("other")); ok {
		t.Fatal("expected false for non-APIError")
	}
}
