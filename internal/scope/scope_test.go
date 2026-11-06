package scope

import "testing"

func TestGameDisplayName_Forever(t *testing.T) {
	t.Parallel()
	if got := GameDisplayName("forever"); got != "WoW Forever" {
		t.Fatalf("got %q", got)
	}
	if got := Label("forever", "us"); got != "All WoW Forever servers — US" {
		t.Fatalf("got %q", got)
	}
}
