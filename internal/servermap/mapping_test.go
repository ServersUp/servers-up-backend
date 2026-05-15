package servermap

import (
	"errors"
	"testing"
)

func TestHumanLabel_match(t *testing.T) {
	t.Parallel()
	m := Mapping{
		Games: map[string]Game{
			"wow": {
				Provider: "battlenet",
				Servers: map[string]Server{
					"illidan": {Region: "us", Identifier: 57},
				},
			},
		},
	}
	got := m.HumanLabel("battlenet#us#57")
	if got != "wow-illidan" {
		t.Fatalf("got %q, want wow-illidan", got)
	}
}

func TestHumanLabel_malformedID(t *testing.T) {
	t.Parallel()
	m := Mapping{Games: map[string]Game{"wow": {Provider: "battlenet", Servers: map[string]Server{}}}}
	raw := "not-a-technical-id"
	if got := m.HumanLabel(raw); got != raw {
		t.Fatalf("got %q, want unchanged %q", got, raw)
	}
}

func TestHumanLabel_unknownID(t *testing.T) {
	t.Parallel()
	m := Mapping{
		Games: map[string]Game{
			"wow": {Provider: "battlenet", Servers: map[string]Server{"a": {Region: "us", Identifier: 1}}},
		},
	}
	raw := "battlenet#eu#999"
	if got := m.HumanLabel(raw); got != raw {
		t.Fatalf("got %q, want unchanged %q", got, raw)
	}
}

func TestHumanLabel_sameProviderTwoGames(t *testing.T) {
	t.Parallel()
	m := Mapping{
		Games: map[string]Game{
			"wow": {Provider: "battlenet", Servers: map[string]Server{"a": {Region: "us", Identifier: 1}}},
			"wipe": {Provider: "battlenet", Servers: map[string]Server{"b": {Region: "us", Identifier: 2}}},
		},
	}
	if got := m.HumanLabel("battlenet#us#2"); got != "wipe-b" {
		t.Fatalf("got %q, want wipe-b", got)
	}
}

func TestNormalizeKey(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"Area 52", "area-52"},
		{"AREA_52", "area-52"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := NormalizeKey(tc.in); got != tc.want {
			t.Errorf("NormalizeKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLookup_errors(t *testing.T) {
	t.Parallel()
	m := Mapping{Games: map[string]Game{"wow": {Provider: "battlenet", Servers: map[string]Server{"a": {Region: "us", Identifier: 1}}}}}
	if _, _, _, _, err := m.Lookup("", "a"); !errors.Is(err, ErrMissingGame) {
		t.Fatalf("missing game: %v", err)
	}
	if _, _, _, _, err := m.Lookup("wow", ""); !errors.Is(err, ErrMissingServer) {
		t.Fatalf("missing server: %v", err)
	}
	if _, _, _, _, err := m.Lookup("nope", "a"); !errors.Is(err, ErrUnknownGame) {
		t.Fatalf("unknown game: %v", err)
	}
	if _, _, _, _, err := m.Lookup("wow", "nope"); !errors.Is(err, ErrUnknownServer) {
		t.Fatalf("unknown server: %v", err)
	}
}

func TestListGames_and_ListServers(t *testing.T) {
	t.Parallel()
	m := Mapping{
		Games: map[string]Game{
			"zeta": {Servers: map[string]Server{"s1": {}}},
			"alpha": {Servers: map[string]Server{"s2": {}}},
		},
	}
	games := m.ListGames()
	if len(games) != 2 || games[0] != "alpha" || games[1] != "zeta" {
		t.Fatalf("ListGames: %#v", games)
	}
	servers, err := m.ListServers("alpha")
	if err != nil || len(servers) != 1 || servers[0] != "s2" {
		t.Fatalf("ListServers: %#v %v", servers, err)
	}
}
