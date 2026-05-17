package discordbot

import (
	"strings"
	"testing"
)

func TestFormatInlineServerListMessage(t *testing.T) {
	t.Parallel()
	msg := formatInlineServerListMessage("wipe", []string{"alpha", "beta"})
	if !strings.Contains(msg, "**Servers for `wipe`** (2)") {
		t.Fatalf("unexpected header: %q", msg)
	}
	if !strings.Contains(msg, "`alpha`") || !strings.Contains(msg, "`beta`") {
		t.Fatalf("expected servers listed: %q", msg)
	}
}

func TestFormatLongServerListMessage_wowPopular(t *testing.T) {
	t.Parallel()
	all := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		all = append(all, "realm-"+strings.Repeat("x", 1))
	}
	all[0] = "illidan"
	all[1] = "area-52"
	all[2] = "unknown-realm"

	msg := formatLongServerListMessage("wow", all)
	if !strings.Contains(msg, "too long") {
		t.Fatalf("expected too long message: %q", msg)
	}
	if !strings.Contains(msg, "`illidan`") || !strings.Contains(msg, "`area-52`") {
		t.Fatalf("expected popular realms: %q", msg)
	}
	if strings.Contains(msg, "`unknown-realm`") {
		t.Fatalf("popular list should only include configured keys: %q", msg)
	}
	if !strings.Contains(msg, supportedGamesListURL) {
		t.Fatalf("expected website link: %q", msg)
	}
}

func TestFormatLongServerListMessage_nonWow(t *testing.T) {
	t.Parallel()
	all := make([]string, 26)
	for i := range all {
		all[i] = "srv"
	}
	msg := formatLongServerListMessage("other", all)
	if !strings.Contains(msg, supportedGamesListURL) {
		t.Fatalf("expected website link: %q", msg)
	}
	if strings.Contains(msg, "Popular US realms") {
		t.Fatalf("non-wow should not show wow popular list: %q", msg)
	}
}
