package servermap

import "testing"

func TestNormalizeGameKey(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"wow-forever": "forever", "WoW Forever": "forever", "wow_forever": "forever", " WOW-FOREVER ": "forever",
		"forever": "forever", "wow": "wow", "ffxiv": "ffxiv", "": "",
	}
	for in, want := range cases {
		if got := NormalizeGameKey(in); got != want {
			t.Errorf("NormalizeGameKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDisplayGameKeyAndLabel(t *testing.T) {
	t.Parallel()
	if DisplayGameKey("forever") != "wow-forever" || DisplayGameKey("wow") != "wow" {
		t.Fatal("unexpected display keys")
	}
	if got := DisplayLabel("forever", "us", "roleplay-pvp"); got != "wow-forever-us-roleplay-pvp" {
		t.Fatalf("got %q", got)
	}
	if got := DisplayLabel("wow", "us", "area-52"); got != "wow-us-area-52" {
		t.Fatalf("got %q", got)
	}
	// Sorted by display name, so wow-forever follows wow.
	if got := DisplayGameKeys([]string{"wow", "forever", "ffxiv"}); got[0] != "ffxiv" || got[1] != "wow" || got[2] != "wow-forever" {
		t.Fatalf("got %v", got)
	}
}

func TestSplitDisplayLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, game, region, server string
		ok                       bool
	}{
		{"wow-forever-us-normal", "wow-forever", "us", "normal", true},
		{"wow-forever-us-roleplay-pvp", "wow-forever", "us", "roleplay-pvp", true},
		{"forever-us-pvp", "forever", "us", "pvp", true}, // label saved before the rename
		{"wow-us-area-52", "wow", "us", "area-52", true},
		{"wow-illidan", "", "", "", false}, // legacy label without a region
		{"wow-forever-us", "wow-forever", "us", "", false},
		{"nohyphen", "", "", "", false},
	}
	for _, tc := range cases {
		g, r, s, ok := SplitDisplayLabel(tc.in)
		if ok != tc.ok || (ok && (g != tc.game || r != tc.region || s != tc.server)) {
			t.Errorf("SplitDisplayLabel(%q) = (%q,%q,%q,%v), want (%q,%q,%q,%v)", tc.in, g, r, s, ok, tc.game, tc.region, tc.server, tc.ok)
		}
	}
}

func TestMappingLookupAcceptsGameAlias(t *testing.T) {
	t.Parallel()
	m := foreverMapping()
	gameID, regionKey, serverKey, _, _, err := m.Lookup("wow-forever", "us", "Normal")
	if err != nil || gameID != "forever" || regionKey != "us" || serverKey != "normal" {
		t.Fatalf("got %q %q %q %v", gameID, regionKey, serverKey, err)
	}
	if regions, err := m.ListRegions("wow-forever"); err != nil || len(regions) != 2 {
		t.Fatalf("regions %v %v", regions, err)
	}
	if servers, err := m.ListServers("wow-forever", "kr"); err != nil || len(servers) != 2 {
		t.Fatalf("servers %v %v", servers, err)
	}
}
