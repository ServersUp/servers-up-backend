package serverid

import "testing"

func TestGenerate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		provider   string
		region     string
		identifier any
		want       string
	}{
		{"int identifier", "battlenet", "us", 57, "battlenet#us#57"},
		{"string identifier", "battlenet", "eu", "realm-a", "battlenet#eu#realm-a"},
		{"zero int", "battlenet", "kr", 0, "battlenet#kr#0"},
		{"empty strings", "", "", "", "##"},
		// Hyphenated server keys (e.g. area-52) must not break the format.
		{"hyphenated server key", "battlenet", "us", "area-52", "battlenet#us#area-52"},
		// Float formats via %v.
		{"float64 identifier", "svc", "us", 3.14, "svc#us#3.14"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Generate(tc.provider, tc.region, tc.identifier)
			if got != tc.want {
				t.Errorf("Generate(%q, %q, %v) = %q; want %q",
					tc.provider, tc.region, tc.identifier, got, tc.want)
			}
		})
	}
}
