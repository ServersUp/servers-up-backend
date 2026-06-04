package ffxivlodestone

import (
	"fmt"
	"sort"

	"github.com/ServersUp/servers-up-backend/internal/servermap"
)

// BuildConfig groups entries into a poller config file.
func BuildConfig(entries []WorldEntry, lodestoneURL string, pollingInterval int) (Config, error) {
	if lodestoneURL == "" {
		return Config{}, fmt.Errorf("lodestone URL is required")
	}
	if pollingInterval <= 0 {
		return Config{}, fmt.Errorf("polling interval must be positive")
	}
	regions, err := groupByRegion(entries)
	if err != nil {
		return Config{}, err
	}
	return Config{
		LodestoneURL:           lodestoneURL,
		FrontierStatusURL:      DefaultFrontierStatusURL,
		PollingIntervalSeconds: pollingInterval,
		Regions:                regions,
	}, nil
}

// BuildGameMapping builds the ffxiv game entry for server-mapping.json.
func BuildGameMapping(entries []WorldEntry, provider string) (servermap.Game, error) {
	if provider == "" {
		provider = "lodestone"
	}
	grouped, err := groupByRegion(entries)
	if err != nil {
		return servermap.Game{}, err
	}
	regions := make(map[string]servermap.Region, len(grouped))
	for region, rc := range grouped {
		servers := make(map[string]servermap.Server, len(rc.Worlds))
		for _, w := range rc.Worlds {
			servers[w.Slug] = servermap.Server{Identifier: w.Name}
		}
		regions[region] = servermap.Region{Servers: servers}
	}
	return servermap.Game{Provider: provider, Regions: regions}, nil
}

func groupByRegion(entries []WorldEntry) (map[string]RegionConfig, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("no world entries")
	}
	m := make(map[string]RegionConfig)
	for _, e := range entries {
		if e.Region == "" || e.Name == "" {
			return nil, fmt.Errorf("entry missing region or name: %+v", e)
		}
		slug := servermap.NormalizeKey(e.Name)
		if slug == "" {
			return nil, fmt.Errorf("empty slug for world %q", e.Name)
		}
		rc := m[e.Region]
		rc.Worlds = append(rc.Worlds, WorldRef{Slug: slug, Name: e.Name})
		m[e.Region] = rc
	}
	for region := range m {
		sort.Slice(m[region].Worlds, func(i, j int) bool {
			return m[region].Worlds[i].Slug < m[region].Worlds[j].Slug
		})
	}
	return m, nil
}

// Summary holds parse statistics for CLI output.
type Summary struct {
	Total       int
	ByRegion    map[string]int
	ByIcon      map[string]int
}

// SummarizeEntries builds counts for logging.
func SummarizeEntries(entries []WorldEntry) Summary {
	s := Summary{
		ByRegion: make(map[string]int),
		ByIcon:   make(map[string]int),
	}
	for _, e := range entries {
		s.Total++
		s.ByRegion[e.Region]++
		s.ByIcon[e.Icon.String()]++
	}
	return s
}

// ValidateExpectedRegions ensures all required physical regions are present.
func ValidateExpectedRegions(entries []WorldEntry, want []string) error {
	got := make(map[string]int)
	for _, e := range entries {
		got[e.Region]++
	}
	for _, r := range want {
		if got[r] == 0 {
			return fmt.Errorf("region %q: no worlds parsed", r)
		}
	}
	return nil
}
