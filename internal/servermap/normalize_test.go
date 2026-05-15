package servermap

import "testing"

func TestNormalizeKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"Area 52", "area-52"},
		{"AREA_52", "area-52"},
		{"  Illidan ", "illidan"},
		{"", ""},
		{"---", ""},
		{"foo__bar", "foo-bar"},
	}
	for _, tc := range cases {
		if got := NormalizeKey(tc.in); got != tc.want {
			t.Errorf("NormalizeKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
