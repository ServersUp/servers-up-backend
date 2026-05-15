package discordbot

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/ServersUp/servers-up-backend/internal/discord"
	"github.com/aws/aws-lambda-go/events"
)

func (h *Handler) getOption(options []discord.InteractionOption, name string) string {
	for _, opt := range options {
		if opt.Name == name {
			if val, ok := opt.Value.(string); ok {
				return val
			}
		}
	}
	return ""
}

func (h *Handler) autocompleteResponse(choices []discord.ApplicationCommandOptionChoice) (events.LambdaFunctionURLResponse, error) {
	if choices == nil {
		choices = []discord.ApplicationCommandOptionChoice{}
	}
	return h.jsonResponse(http.StatusOK, discord.InteractionResponse{
		Type: discord.InteractionResponseTypeApplicationCommandAutocompleteResult,
		Data: &discord.InteractionResponseData{Choices: choices},
	})
}

func (h *Handler) discordResponse(content string) (events.LambdaFunctionURLResponse, error) {
	return h.jsonResponse(http.StatusOK, discord.InteractionResponse{
		Type: discord.InteractionResponseTypeChannelMessageWithSource,
		Data: &discord.InteractionResponseData{
			Content: content,
		},
	})
}

func (h *Handler) jsonResponse(statusCode int, body any) (events.LambdaFunctionURLResponse, error) {
	jsonBytes, err := json.Marshal(body)
	if err != nil {
		slog.Error("failed to marshal interaction response", "error", err)
		return events.LambdaFunctionURLResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       `{"error":"internal"}`,
			Headers:    map[string]string{"Content-Type": "application/json"},
		}, nil
	}
	return events.LambdaFunctionURLResponse{
		StatusCode: statusCode,
		Body:       string(jsonBytes),
		Headers:    map[string]string{"Content-Type": "application/json"},
	}, nil
}
