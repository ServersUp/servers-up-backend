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
