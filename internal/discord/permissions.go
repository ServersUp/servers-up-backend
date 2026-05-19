package discord

import "strconv"

const (
	// PermissionAdministrator allows all guild permissions.
	PermissionAdministrator int64 = 1 << 3 // 0x8
	// PermissionManageChannels is required for channel-scoped bot configuration.
	PermissionManageChannels int64 = 1 << 4 // 0x10
)

// CanManageSubscriptions reports whether the member may run /subscribe or /unsubscribe.
func CanManageSubscriptions(permissionsBitfield string) bool {
	if permissionsBitfield == "" {
		return false
	}
	p, err := strconv.ParseInt(permissionsBitfield, 10, 64)
	if err != nil {
		return false
	}
	if p&PermissionAdministrator != 0 {
		return true
	}
	return p&PermissionManageChannels != 0
}
