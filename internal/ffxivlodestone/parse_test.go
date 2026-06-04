package ffxivlodestone

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ServersUp/servers-up-backend/internal/servermap"
)

func TestParseWorldStatusHTML_fixture(t *testing.T) {
	t.Parallel()
	html, err := os.ReadFile(filepath.Join("testdata", "worldstatus.html"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := ParseWorldStatusHTML(html)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 80 {
		t.Fatalf("expected at least 80 worlds, got %d", len(entries))
	}
	if err := ValidateExpectedRegions(entries, []string{"na", "eu", "jp", "oce"}); err != nil {
		t.Fatal(err)
	}

	byRegion := make(map[string][]WorldEntry)
	for _, e := range entries {
		byRegion[e.Region] = append(byRegion[e.Region], e)
	}
	if len(byRegion["na"]) < 15 {
		t.Fatalf("na: expected many worlds, got %d", len(byRegion["na"]))
	}

	var gilgamesh *WorldEntry
	for i := range entries {
		if entries[i].Name == "Gilgamesh" && entries[i].Region == "na" {
			gilgamesh = &entries[i]
			break
		}
	}
	if gilgamesh == nil {
		t.Fatal("expected Gilgamesh in na")
	}
	if gilgamesh.Icon != IconOnline {
		t.Fatalf("Gilgamesh icon: got %v want online", gilgamesh.Icon)
	}

	var odin *WorldEntry
	for i := range entries {
		if entries[i].Name == "Odin" && entries[i].Region == "eu" {
			odin = &entries[i]
			break
		}
	}
	if odin == nil {
		t.Fatal("expected Odin in eu")
	}
	if odin.Icon != IconOnline {
		t.Fatalf("Odin icon: got %v", odin.Icon)
	}
}

func TestBuildGameMapping_lookup(t *testing.T) {
	t.Parallel()
	html, err := os.ReadFile(filepath.Join("testdata", "worldstatus.html"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := ParseWorldStatusHTML(html)
	if err != nil {
		t.Fatal(err)
	}
	game, err := BuildGameMapping(entries, "lodestone")
	if err != nil {
		t.Fatal(err)
	}
	m := servermap.Mapping{Games: map[string]servermap.Game{"ffxiv": game}}
	_, _, _, _, _, err = m.Lookup("ffxiv", "na", "gilgamesh")
	if err != nil {
		t.Fatalf("lookup gilgamesh: %v", err)
	}
}

func TestBuildConfig(t *testing.T) {
	t.Parallel()
	html, err := os.ReadFile(filepath.Join("testdata", "worldstatus.html"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := ParseWorldStatusHTML(html)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := BuildConfig(entries, "https://na.finalfantasyxiv.com/lodestone/worldstatus/", 60)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PollingIntervalSeconds != 60 {
		t.Fatalf("interval: %d", cfg.PollingIntervalSeconds)
	}
	if len(cfg.Regions["jp"].Worlds) == 0 {
		t.Fatal("expected jp worlds")
	}
}

func TestStatusIconFromItem_classes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		class string
		want  IconStatus
	}{
		{"world-ic__1 js__tooltip", IconOnline},
		{"world-ic__2 js__tooltip", IconPartialMaintenance},
		{"world-ic__3 js__tooltip", IconMaintenance},
	}
	for _, tt := range tests {
		got, err := statusIconFromClass(tt.class)
		if err != nil {
			t.Fatalf("%q: %v", tt.class, err)
		}
		if got != tt.want {
			t.Fatalf("%q: got %v want %v", tt.class, got, tt.want)
		}
	}
}

func statusIconFromClass(cls string) (IconStatus, error) {
	switch {
	case hasToken(cls, "world-ic__1"):
		return IconOnline, nil
	case hasToken(cls, "world-ic__2"):
		return IconPartialMaintenance, nil
	case hasToken(cls, "world-ic__3"):
		return IconMaintenance, nil
	default:
		return 0, fmt.Errorf("unrecognized %q", cls)
	}
}
