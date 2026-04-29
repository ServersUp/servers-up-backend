package discord

import (
	"encoding/json"
)

// InteractionType represents the type of interaction received from Discord.
type InteractionType int

const (
	InteractionTypePing                           InteractionType = 1
	InteractionTypeApplicationCommand             InteractionType = 2
	InteractionTypeMessageComponent               InteractionType = 3
	InteractionTypeApplicationCommandAutocomplete InteractionType = 4
	InteractionTypeModalSubmit                    InteractionType = 5
)

// InteractionResponseType represents the type of response to send back to Discord.
type InteractionResponseType int

const (
	InteractionResponseTypePong                                 InteractionResponseType = 1
	InteractionResponseTypeChannelMessageWithSource             InteractionResponseType = 4
	InteractionResponseTypeDeferredChannelMessageWithSource     InteractionResponseType = 5
	InteractionResponseTypeDeferredUpdateMessage                InteractionResponseType = 6
	InteractionResponseTypeUpdateMessage                        InteractionResponseType = 7
	InteractionResponseTypeApplicationCommandAutocompleteResult InteractionResponseType = 8
	InteractionResponseTypeModal                                InteractionResponseType = 9
)

// Interaction represents the base interaction object received from Discord.
type Interaction struct {
	ID            string          `json:"id"`
	ApplicationID string          `json:"application_id"`
	Type          InteractionType `json:"type"`
	Data          json.RawMessage `json:"data"`
	GuildID       string          `json:"guild_id"`
	ChannelID     string          `json:"channel_id"`
	Token         string          `json:"token"`
	Version       int             `json:"version"`
}

// InteractionData represents the command-specific data in an interaction.
type InteractionData struct {
	ID       string              `json:"id"`
	Name     string              `json:"name"`
	Type     int                 `json:"type"`
	Options  []InteractionOption `json:"options"`
	GuildID  string              `json:"guild_id"`
	TargetID string              `json:"target_id"`
}

// InteractionOption represents a command option/argument.
type InteractionOption struct {
	Type    int                 `json:"type"`
	Name    string              `json:"name"`
	Value   any                 `json:"value"`
	Options []InteractionOption `json:"options"`
}

// InteractionResponse represents the response sent back to Discord.
type InteractionResponse struct {
	Type InteractionResponseType  `json:"type"`
	Data *InteractionResponseData `json:"data,omitempty"`
}

// InteractionResponseData represents the content of a Discord response.
type InteractionResponseData struct {
	Content string `json:"content"`
	// Flags can be used to control response visibility (e.g., 64 for ephemeral).
	Flags int `json:"flags,omitempty"`
}
