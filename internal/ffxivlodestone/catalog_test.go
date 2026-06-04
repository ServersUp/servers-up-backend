package ffxivlodestone

import "testing"

func TestListCatalogWorlds(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Regions: map[string]RegionConfig{
			"na": {Worlds: []WorldRef{{Slug: "a", Name: "Alpha"}}},
			"eu": {Worlds: []WorldRef{{Slug: "b", Name: "Beta"}}},
		},
	}
	worlds, err := ListCatalogWorlds(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(worlds) != 2 {
		t.Fatalf("got %d worlds", len(worlds))
	}
}
