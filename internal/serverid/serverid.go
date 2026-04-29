package serverid

import "fmt"

// Generate returns a stable server identifier used across the system.
// Format: provider#region#identifier (e.g. battlenet#us#57)
func Generate(provider, region string, identifier any) string {
	return fmt.Sprintf("%s#%s#%v", provider, region, identifier)
}
