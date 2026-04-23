package bnet

// Config represents the schema for the Battle.net server configuration stored in S3.
type Config struct {
	Region                 string        `json:"region"`
	Locale                 string        `json:"locale"`
	Realms                 []RealmConfig `json:"realms"`
	PollingIntervalSeconds int           `json:"polling_interval_seconds"`
}

// RealmConfig defines the metadata for a specific realm to be polled.
type RealmConfig struct {
	Name             string `json:"name"`
	Slug             string `json:"slug"`
	ConnectedRealmID int    `json:"connected_realm_id"`
}

// BNetTokenResponse is the standard OAuth2 token response from Battle.net.
type BNetTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// ConnectedRealmResponse maps the Blizzard API response for a connected realm.
// Only includes fields necessary for status monitoring to keep memory usage low.
type ConnectedRealmResponse struct {
	ID     int    `json:"id"`
	Status Status `json:"status"`
}

type Status struct {
	Type string `json:"type"` // e.g., "UP", "DOWN"
	Name string `json:"name"`
}
