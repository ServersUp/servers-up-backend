package servermap

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestDisplayLabel(t *testing.T) {
	t.Parallel()
	if got := DisplayLabel("wow", "eu", "kazzak"); got != "wow-eu-kazzak" {
		t.Fatalf("got %q, want wow-eu-kazzak", got)
	}
	if got := DisplayLabel("wow", "us", "illidan"); got != "wow-us-illidan" {
		t.Fatalf("got %q, want wow-us-illidan", got)
	}
}

func testMapping() Mapping {
	return Mapping{
		Games: map[string]Game{
			"wow": {
				Provider: "battlenet",
				Regions: map[string]Region{
					"us": {Servers: map[string]Server{
						"illidan": {Identifier: 57},
						"area-52": {Identifier: 1551},
					}},
					"eu": {Servers: map[string]Server{
						"kazzak":      {Identifier: 1305},
						"argent-dawn": {Identifier: 1391},
					}},
				},
			},
			"wipe": {
				Provider: "battlenet",
				Regions: map[string]Region{
					"us": {Servers: map[string]Server{
						"b": {Identifier: 2},
					}},
				},
			},
		},
	}
}

func TestHumanLabel_match(t *testing.T) {
	t.Parallel()
	m := testMapping()
	if got := m.HumanLabel("battlenet#us#57"); got != "wow-us-illidan" {
		t.Fatalf("got %q, want wow-us-illidan", got)
	}
}

func TestHumanLabel_euMatch(t *testing.T) {
	t.Parallel()
	m := testMapping()
	if got := m.HumanLabel("battlenet#eu#1305"); got != "wow-eu-kazzak" {
		t.Fatalf("got %q, want wow-eu-kazzak", got)
	}
}

func TestHumanLabel_malformedID(t *testing.T) {
	t.Parallel()
	m := testMapping()
	raw := "not-a-technical-id"
	if got := m.HumanLabel(raw); got != raw {
		t.Fatalf("got %q, want unchanged %q", got, raw)
	}
}

func TestHumanLabel_unknownID(t *testing.T) {
	t.Parallel()
	m := testMapping()
	raw := "battlenet#eu#999"
	if got := m.HumanLabel(raw); got != raw {
		t.Fatalf("got %q, want unchanged %q", got, raw)
	}
}

func TestHumanLabel_unknownRegion(t *testing.T) {
	t.Parallel()
	m := testMapping()
	// kr region not in testMapping; should fall back to raw id
	raw := "battlenet#kr#214"
	if got := m.HumanLabel(raw); got != raw {
		t.Fatalf("got %q, want unchanged %q", got, raw)
	}
}

func TestHumanLabel_sameProviderTwoGames(t *testing.T) {
	t.Parallel()
	m := testMapping()
	if got := m.HumanLabel("battlenet#us#2"); got != "wipe-us-b" {
		t.Fatalf("got %q, want wipe-us-b", got)
	}
}

// TestHumanLabel_sameServerKeyDifferentRegions verifies that US and EU argent-dawn
// with different identifiers resolve to distinct display labels.
func TestHumanLabel_sameServerKeyDifferentRegions(t *testing.T) {
	t.Parallel()
	m := Mapping{
		Games: map[string]Game{
			"wow": {
				Provider: "battlenet",
				Regions: map[string]Region{
					"us": {Servers: map[string]Server{"argent-dawn": {Identifier: 61}}},
					"eu": {Servers: map[string]Server{"argent-dawn": {Identifier: 1391}}},
				},
			},
		},
	}
	if got := m.HumanLabel("battlenet#us#61"); got != "wow-us-argent-dawn" {
		t.Fatalf("US: got %q, want wow-us-argent-dawn", got)
	}
	if got := m.HumanLabel("battlenet#eu#1391"); got != "wow-eu-argent-dawn" {
		t.Fatalf("EU: got %q, want wow-eu-argent-dawn", got)
	}
}

func TestNormalizeKey(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"Area 52", "area-52"},
		{"AREA_52", "area-52"},
		{"  Illidan ", "illidan"},
		{"", ""},
		{"---", ""},
		{"foo__bar", "foo-bar"},
	}
	for _, tc := range cases {
		if got := NormalizeKey(tc.in); got != tc.want {
			t.Errorf("NormalizeKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLookup_errors(t *testing.T) {
	t.Parallel()
	m := testMapping()
	if _, _, _, _, _, err := m.Lookup("", "us", "a"); !errors.Is(err, ErrMissingGame) {
		t.Fatalf("missing game: %v", err)
	}
	if _, _, _, _, _, err := m.Lookup("wow", "", "a"); !errors.Is(err, ErrMissingRegion) {
		t.Fatalf("missing region: %v", err)
	}
	if _, _, _, _, _, err := m.Lookup("wow", "us", ""); !errors.Is(err, ErrMissingServer) {
		t.Fatalf("missing server: %v", err)
	}
	if _, _, _, _, _, err := m.Lookup("nope", "us", "a"); !errors.Is(err, ErrUnknownGame) {
		t.Fatalf("unknown game: %v", err)
	}
	if _, _, _, _, _, err := m.Lookup("wow", "kr", "a"); !errors.Is(err, ErrUnknownRegion) {
		t.Fatalf("unknown region: %v", err)
	}
	if _, _, _, _, _, err := m.Lookup("wow", "us", "nope"); !errors.Is(err, ErrUnknownServer) {
		t.Fatalf("unknown server: %v", err)
	}
}

func TestLookup_found(t *testing.T) {
	t.Parallel()
	m := testMapping()
	gameID, regionKey, serverKey, game, server, err := m.Lookup("wow", "eu", "kazzak")
	if err != nil {
		t.Fatal(err)
	}
	if gameID != "wow" || regionKey != "eu" || serverKey != "kazzak" {
		t.Fatalf("keys: %s %s %s", gameID, regionKey, serverKey)
	}
	if game.Provider != "battlenet" {
		t.Fatalf("provider: %s", game.Provider)
	}
	if server.Identifier != 1305 {
		t.Fatalf("identifier: %v", server.Identifier)
	}
}

func TestListGames(t *testing.T) {
	t.Parallel()
	m := testMapping()
	games := m.ListGames()
	if len(games) != 2 || games[0] != "wipe" || games[1] != "wow" {
		t.Fatalf("ListGames: %#v", games)
	}
}

func TestListRegions(t *testing.T) {
	t.Parallel()
	m := testMapping()
	regions, err := m.ListRegions("wow")
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 2 || regions[0] != "eu" || regions[1] != "us" {
		t.Fatalf("ListRegions: %#v", regions)
	}
	if _, err := m.ListRegions(""); !errors.Is(err, ErrMissingGame) {
		t.Fatalf("missing game: %v", err)
	}
	if _, err := m.ListRegions("nope"); !errors.Is(err, ErrUnknownGame) {
		t.Fatalf("unknown game: %v", err)
	}
}

func TestListServers(t *testing.T) {
	t.Parallel()
	m := testMapping()
	servers, err := m.ListServers("wow", "us")
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 || servers[0] != "area-52" || servers[1] != "illidan" {
		t.Fatalf("ListServers: %#v", servers)
	}
	if _, err := m.ListServers("wow", ""); !errors.Is(err, ErrMissingRegion) {
		t.Fatalf("missing region: %v", err)
	}
	if _, err := m.ListServers("wow", "kr"); !errors.Is(err, ErrUnknownRegion) {
		t.Fatalf("unknown region: %v", err)
	}
}

func TestLoadFromTestdata(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/server-mapping.json")
	if err != nil {
		t.Fatal(err)
	}
	var m Mapping
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}

	// US argent-dawn and EU argent-dawn must have different identifiers.
	usServers := m.Games["wow"].Regions["us"].Servers
	euServers := m.Games["wow"].Regions["eu"].Servers
	usID := usServers["argent-dawn"].Identifier
	euID := euServers["argent-dawn"].Identifier
	if usID == nil || euID == nil {
		t.Fatalf("argent-dawn missing in US or EU")
	}
	if usID == euID {
		t.Fatalf("argent-dawn US and EU identifiers must differ, both got %v", usID)
	}

	// Lookup should resolve eu/argent-dawn and us/argent-dawn independently.
	_, regionKey, serverKey, _, _, err := m.Lookup("wow", "eu", "argent-dawn")
	if err != nil || regionKey != "eu" || serverKey != "argent-dawn" {
		t.Fatalf("eu argent-dawn lookup: %s %s %v", regionKey, serverKey, err)
	}
	_, regionKey, serverKey, _, _, err = m.Lookup("wow", "us", "argent-dawn")
	if err != nil || regionKey != "us" || serverKey != "argent-dawn" {
		t.Fatalf("us argent-dawn lookup: %s %s %v", regionKey, serverKey, err)
	}
}
