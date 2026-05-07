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
}

// GuildNotifyJob is the payload sent to the Discord guild notify SQS queue when
// a watched game server transitions between UP and DOWN (or similar states).
type GuildNotifyJob struct {
	ServerID  string `json:"serverId"`
	Status    string `json:"status"`
	GuildID   string `json:"guildId"`
	ChannelID string `json:"channelId"`
	RoleID    string `json:"roleId,omitempty"`
}
