package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const apiBase = "https://discord.com/api/v10"

type apiRole struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type apiChannel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GuildRoleName returns the role's display name for a role ID in a guild.
func GuildRoleName(ctx context.Context, client *http.Client, botToken, guildID, roleID string) (string, error) {
	if botToken == "" || guildID == "" || roleID == "" {
		return "", fmt.Errorf("discord: missing guild, token, or role id")
	}
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/guilds/"+guildID+"/roles", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bot "+strings.TrimSpace(botToken))
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("discord: guild roles %s: %s", resp.Status, truncateForErr(body))
	}
	var roles []apiRole
	if err := json.Unmarshal(body, &roles); err != nil {
		return "", fmt.Errorf("discord: decode roles: %w", err)
	}
	for _, r := range roles {
		if r.ID == roleID {
			return r.Name, nil
		}
	}
	return "", fmt.Errorf("discord: role %s not in guild", roleID)
}

// GuildChannelNames returns a map of channel ID to channel name for all guild channels.
func GuildChannelNames(ctx context.Context, client *http.Client, botToken, guildID string) (map[string]string, error) {
	if botToken == "" || guildID == "" {
		return nil, fmt.Errorf("discord: missing guild or token")
	}
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/guilds/"+guildID+"/channels", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bot "+strings.TrimSpace(botToken))
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discord: guild channels %s: %s", resp.Status, truncateForErr(body))
	}
	var channels []apiChannel
	if err := json.Unmarshal(body, &channels); err != nil {
		return nil, fmt.Errorf("discord: decode channels: %w", err)
	}
	out := make(map[string]string, len(channels))
	for _, ch := range channels {
		if ch.ID != "" && ch.Name != "" {
			out[ch.ID] = ch.Name
		}
	}
	return out, nil
}

func truncateForErr(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
