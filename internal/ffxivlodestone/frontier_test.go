package ffxivlodestone

import "testing"

func TestStatusFromFrontierCode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code   int
		want   string
		wantOK bool
	}{
		{1, "UP", true},
		{0, "DOWN", true},
		{2, "DOWN", true},
		{3, "DOWN", true},
		{9, "", false},
	}
	for _, tc := range cases {
		got, err := StatusFromFrontierCode(tc.code)
		if tc.wantOK && err != nil {
			t.Fatalf("code %d: %v", tc.code, err)
		}
		if !tc.wantOK && err == nil {
			t.Fatalf("code %d: expected error", tc.code)
		}
		if tc.wantOK && got != tc.want {
			t.Fatalf("code %d: got %q want %q", tc.code, got, tc.want)
		}
	}
}

func TestParseFrontierStatusJSON(t *testing.T) {
	t.Parallel()
	body := []byte(`{"Gilgamesh":1,"Mateus":0}`)
	m, err := ParseFrontierStatusJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	if m["Gilgamesh"] != 1 || m["Mateus"] != 0 {
		t.Fatalf("map: %+v", m)
	}
}
