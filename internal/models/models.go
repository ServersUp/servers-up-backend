package models

// GameServerStatus represents the status record stored in DynamoDB.
// It is designed to be provider-agnostic to support multiple games and platforms.
type GameServerStatus struct {
	// GameID is the partition key (e.g., "wow", "ffxiv").
	GameID string `json:"gameId" dynamodbav:"gameId"`
	// ServerID is the sort key, typically a combination of provider, region, and ID.
	ServerID string `json:"serverId" dynamodbav:"serverId"`

	// Provider identifies the source of the data (e.g., "battlenet").
	Provider string `json:"provider" dynamodbav:"provider"`
	// Region identifies the geographical area (e.g., "us", "eu").
	Region string `json:"region" dynamodbav:"region"`

	// Status represents the current state (e.g., UP, DOWN, DEGRADED).
	Status string `json:"status" dynamodbav:"status"`

	// LastUpdatedAt tracks when the status value itself last changed.
	LastUpdatedAt int64 `json:"lastUpdatedAt" dynamodbav:"lastUpdatedAt"`

	// Meta allows for provider-specific or extensible metadata without breaking the schema.
	Meta map[string]any `json:"meta,omitempty" dynamodbav:"meta,omitempty"`
}

// Subscription represents a Discord notification target for a game server.
type Subscription struct {
	// ServerID is the hash key (e.g., "battlenet#us#11").
	ServerID string `json:"server_id" dynamodbav:"serverId"`
	// SubscriptionID is a unique identifier for this specific channel/role combo.
	SubscriptionID string `json:"subscription_id" dynamodbav:"subscriptionId"`

	GuildID   string `json:"guild_id" dynamodbav:"guildId"`
	ChannelID string `json:"channel_id" dynamodbav:"channelId"`
	Mention   string `json:"mention" dynamodbav:"mention"`
	// RoleName is the Discord role display name (resolved at subscribe time when a bot token is configured).
	RoleName string `json:"role_name,omitempty" dynamodbav:"roleName,omitempty"`
	// ServerLabel is the human-readable "game-server" label captured at subscribe time (e.g. "wow-illidan").
	ServerLabel string `json:"server_label,omitempty" dynamodbav:"serverLabel,omitempty"`
	// Scope indicates the target scope: "server" (default), "region", or "game".
	// "region" and "game" are wildcard scopes whose partition key is a reserved
	// "region#<gameId>#<region>" / "game#<gameId>" key (see internal/scope).
	Scope string `json:"scope,omitempty" dynamodbav:"scope,omitempty"`
	// GameID is the game for region/game scopes.
	GameID string `json:"game_id,omitempty" dynamodbav:"gameId,omitempty"`
	// Region is the region for region scopes.
	Region string `json:"region,omitempty" dynamodbav:"region,omitempty"`
	// TargetType indicates the delivery mechanism: "bot" (default) or "webhook".
	TargetType string `json:"target_type" dynamodbav:"targetType"`
	// WebhookURL is used when TargetType is "webhook".
	WebhookURL string `json:"webhook_url,omitempty" dynamodbav:"webhookUrl,omitempty"`
	// WebhookToken is optional metadata for webhook delivery.
	WebhookToken string `json:"webhook_token,omitempty" dynamodbav:"webhookToken,omitempty"`
}

// GuildNotifyJob is the payload sent to the Discord guild notify SQS queue when
// a watched game server transitions between UP and DOWN (or similar states).
type GuildNotifyJob struct {
	ServerID  string `json:"serverId"`
	Status    string `json:"status"`
	GuildID   string `json:"guildId"`
	ChannelID string `json:"channelId"`
	RoleID    string `json:"roleId,omitempty"`
	// ServerLabel is the human-readable "game-server" label captured at subscribe time.
	// When non-empty, the notify lambda uses this directly instead of reverse-mapping the technical ID.
	ServerLabel string `json:"serverLabel,omitempty"`
	// TargetType indicates the delivery mechanism: "bot" (default) or "webhook".
	TargetType string `json:"targetType,omitempty"`
	// WebhookURL is used when TargetType is "webhook".
	WebhookURL string `json:"webhookUrl,omitempty"`
	// Aggregate marks an aggregate scope notification (region/game wildcard)
	// rather than a per-server notification.
	Aggregate bool `json:"aggregate,omitempty"`
	// ScopeLabel is the human scope for aggregate messages, e.g. "WoW" or
	// "WoW EU". Only set when Aggregate is true.
	ScopeLabel string `json:"scopeLabel,omitempty"`
	// Scope is the scope type for aggregate jobs: "region" or "game".
	Scope string `json:"scope,omitempty"`
}

// ScopeState tracks the aggregate state of a region/game subscription scope.
// One row exists per currently subscribed wildcard scope.
type ScopeState struct {
	// ScopeKey is the wildcard partition key ("region#<gameId>#<region>" /
	// "game#<gameId>").
	ScopeKey string `json:"scopeKey" dynamodbav:"scopeKey"`
	// UpCount is the number of catalog members currently UP.
	UpCount int `json:"upCount" dynamodbav:"upCount"`
	// DownCount is the number of catalog members currently not-UP.
	DownCount int `json:"downCount" dynamodbav:"downCount"`
	// TotalCount is the number of catalog members for the scope.
	TotalCount int `json:"totalCount" dynamodbav:"totalCount"`
	// State is the aggregate state: ALL_UP, MIXED, or ALL_DOWN.
	State string `json:"state" dynamodbav:"state"`
	// StateSince is the unix time the current State began.
	StateSince int64 `json:"stateSince" dynamodbav:"stateSince"`
	// LastNotifiedEpisode is the episode token last notified for this scope.
	LastNotifiedEpisode string `json:"lastNotifiedEpisode,omitempty" dynamodbav:"lastNotifiedEpisode,omitempty"`
	// UpdatedAt is the unix time of the last write.
	UpdatedAt int64 `json:"updatedAt" dynamodbav:"updatedAt"`
}
