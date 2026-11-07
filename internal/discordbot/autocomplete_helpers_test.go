package discordbot

import (
	"testing"

	"github.com/ServersUp/servers-up-backend/internal/servermap"
)

func TestAutocompleteGames_prefixFilter(t *testing.T) {
	t.Parallel()
	m := servermap.Mapping{Games: map[string]servermap.Game{
		"wow":   {},
		"ffxiv": {},
		"eso":   {},
	}}
	choices := autocompleteGames(m, "w", 25)
	if len(choices) != 1 || choices[0].Value != "wow" {
		t.Fatalf("got %+v", choices)
	}
}

func TestAutocompleteRegions_requiresGame(t *testing.T) {
	t.Parallel()
	m := servermap.Mapping{Games: map[string]servermap.Game{
		"wow": {Regions: map[string]servermap.Region{
			"us": {Servers: map[string]servermap.Server{"illidan": {}}},
		}},
	}}
	choices, err := autocompleteRegions(m, "", "u", 25)
	if err != nil || choices != nil {
		t.Fatalf("got choices=%v err=%v", choices, err)
	}
}

func TestAutocompleteRegions_listsRegions(t *testing.T) {
	t.Parallel()
	m := servermap.Mapping{Games: map[string]servermap.Game{
		"wow": {Regions: map[string]servermap.Region{
			"us": {},
			"eu": {},
			"kr": {},
		}},
	}}
	choices, err := autocompleteRegions(m, "wow", "e", 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(choices) != 1 || choices[0].Value != "eu" {
		t.Fatalf("expected [eu] for prefix e, got %+v", choices)
	}
}

func TestAutocompleteServers_requiresGameAndRegion(t *testing.T) {
	t.Parallel()
	m := servermap.Mapping{Games: map[string]servermap.Game{
		"wow": {Regions: map[string]servermap.Region{
			"us": {Servers: map[string]servermap.Server{"illidan": {}}},
		}},
	}}
	// no game
	choices, err := autocompleteServers(m, "", "us", "ill", 25)
	if err != nil || choices != nil {
		t.Fatalf("no-game: got choices=%v err=%v", choices, err)
	}
	// no region
	choices, err = autocompleteServers(m, "wow", "", "ill", 25)
	if err != nil || choices != nil {
		t.Fatalf("no-region: got choices=%v err=%v", choices, err)
	}
}

func TestAutocompleteServers_listsServers(t *testing.T) {
	t.Parallel()
	m := servermap.Mapping{Games: map[string]servermap.Game{
		"wow": {Regions: map[string]servermap.Region{
			"us": {Servers: map[string]servermap.Server{
				"illidan": {},
				"area-52": {},
			}},
		}},
	}}
	choices, err := autocompleteServers(m, "wow", "us", "ill", 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(choices) != 1 || choices[0].Value != "illidan" {
		t.Fatalf("got %+v", choices)
	}
}

func TestAutocompleteGames_forever(t *testing.T) {
	t.Parallel()
	m := servermap.Mapping{Games: map[string]servermap.Game{"wow": {}, "forever": {}, "ffxiv": {}}}
	names := func(partial string) []string {
		var out []string
		for _, c := range autocompleteGames(m, partial, 25) {
			if c.Name != c.Value {
				t.Fatalf("name and value must match: %+v", c)
			}
			out = append(out, c.Value)
		}
		return out
	}
	cases := []struct {
		partial string
		want    []string
	}{
		{"", []string{"ffxiv", "wow", "wow-forever"}},
		{"w", []string{"wow", "wow-forever"}},
		{"wow-f", []string{"wow-forever"}},
		{"WoW Forever", []string{"wow-forever"}},
		{"forever", []string{"wow-forever"}}, // the internal ID still finds it
		{"fo", []string{"wow-forever"}},
		{"zzz", nil},
	}
	for _, tc := range cases {
		got := names(tc.partial)
		if len(got) != len(tc.want) {
			t.Fatalf("partial %q: got %v want %v", tc.partial, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("partial %q: got %v want %v", tc.partial, got, tc.want)
			}
		}
	}
}
