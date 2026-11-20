package servermap

import (
	"regexp"
	"strings"
)

var sepRe = regexp.MustCompile(`[-_\s]+`)

// NormalizeKey normalizes user input to the slug keys used in server-mapping.json.
// Examples:
// - "Area 52" -> "area-52"
// - "  Illidan " -> "illidan"
func NormalizeKey(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}

	s = sepRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}
