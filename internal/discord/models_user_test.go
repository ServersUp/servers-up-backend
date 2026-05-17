package discord

import "testing"

func TestInteraction_InvokerUserID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   Interaction
		want string
	}{
		{
			name: "guild member",
			in: Interaction{
				Member: &InteractionMember{User: InteractionUser{ID: "111"}},
				User:   &InteractionUser{ID: "222"},
			},
			want: "111",
		},
		{
			name: "dm user",
			in: Interaction{
				User: &InteractionUser{ID: "222"},
			},
			want: "222",
		},
		{
			name: "empty",
			in:   Interaction{},
			want: "",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.in.InvokerUserID(); got != tc.want {
				t.Fatalf("InvokerUserID() = %q want %q", got, tc.want)
			}
		})
	}
}
