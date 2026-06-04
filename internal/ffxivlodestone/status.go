package ffxivlodestone

import "fmt"

// StatusFromIcon maps Lodestone HTML status icons to UP/DOWN (HTML fallback path).
func StatusFromIcon(icon IconStatus) (string, error) {
	switch icon {
	case IconOnline:
		return "UP", nil
	case IconPartialMaintenance, IconMaintenance:
		return "DOWN", nil
	default:
		return "", fmt.Errorf("ffxivlodestone: unmapped icon status %v", icon)
	}
}

// StatusMapFromHTMLEntries builds world name → UP/DOWN from parsed HTML rows.
func StatusMapFromHTMLEntries(entries []WorldEntry) (map[string]string, error) {
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		status, err := StatusFromIcon(e.Icon)
		if err != nil {
			return nil, err
		}
		out[e.Name] = status
	}
	return out, nil
}
