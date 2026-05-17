package discordbot

import (
	"testing"
	"time"
)

func TestFormatStatusLastUpdated(t *testing.T) {
	t.Parallel()
	if got := formatStatusLastUpdated(0); got != "unknown" {
		t.Fatalf("zero: got %q", got)
	}
	got := formatStatusLastUpdated(1710000000)
	want := time.Unix(1710000000, 0).UTC().Format("2006-01-02 15:04 UTC")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
