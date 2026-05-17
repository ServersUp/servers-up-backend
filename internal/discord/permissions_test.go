package discord

import "testing"

func TestCanManageSubscriptions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		perms string
		want  bool
	}{
		{"admin", "8", true},
		{"manage channels", "16", true},
		{"both", "24", true},
		{"send messages only", "2048", false},
		{"empty", "", false},
		{"invalid", "not-a-number", false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := CanManageSubscriptions(tc.perms); got != tc.want {
				t.Fatalf("CanManageSubscriptions(%q) = %v, want %v", tc.perms, got, tc.want)
			}
		})
	}
}
