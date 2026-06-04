package ffxivlodestone

const (
	// DefaultLodestoneURL is the world status page used for HTML fallback and config generation.
	DefaultLodestoneURL = "https://na.finalfantasyxiv.com/lodestone/worldstatus/"
	// DefaultFrontierStatusURL is the launcher-style JSON feed for live world status.
	DefaultFrontierStatusURL = "https://frontier.ffxiv.com/worldStatus/current_status.json"
	// GameID is the DynamoDB gameId partition key for FFXIV status rows.
	GameID = "ffxiv"
	// Provider is the provider segment in serverId and server-mapping.json.
	Provider = "lodestone"
)

// Config is the poller catalog loaded from S3 (written by generate-ffxiv-configs).
type Config struct {
	LodestoneURL           string                  `json:"lodestone_url"`
	FrontierStatusURL      string                  `json:"frontier_status_url"`
	PollingIntervalSeconds int                     `json:"polling_interval_seconds"`
	Regions                map[string]RegionConfig `json:"regions"`
}

// RegionConfig lists worlds in a physical data center (na, eu, jp, oce).
type RegionConfig struct {
	Worlds []WorldRef `json:"worlds"`
}

// WorldRef ties a slug key to the exact Lodestone world name.
type WorldRef struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// ResolvedURLs returns config URLs with package defaults applied.
func (c Config) ResolvedURLs() (lodestone, frontier string) {
	lodestone = c.LodestoneURL
	if lodestone == "" {
		lodestone = DefaultLodestoneURL
	}
	frontier = c.FrontierStatusURL
	if frontier == "" {
		frontier = DefaultFrontierStatusURL
	}
	return lodestone, frontier
}
