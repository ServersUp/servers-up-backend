package ffxivlodestone

import (
	"encoding/json"
	"fmt"
)

// StatusFromFrontierCode maps Square Enix launcher JSON codes to UP/DOWN.
// 1 is online; 0, 2, and 3 are treated as down.
func StatusFromFrontierCode(code int) (string, error) {
	switch code {
	case 1:
		return "UP", nil
	case 0, 2, 3:
		return "DOWN", nil
	default:
		return "", fmt.Errorf("ffxivlodestone: unknown frontier status code %d", code)
	}
}

// ParseFrontierStatusJSON parses the flat world-name → code map from frontier JSON.
func ParseFrontierStatusJSON(body []byte) (map[string]int, error) {
	var raw map[string]int
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse frontier json: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("frontier json: empty map")
	}
	return raw, nil
}
