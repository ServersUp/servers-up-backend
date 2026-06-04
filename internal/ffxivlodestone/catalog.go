package ffxivlodestone

import "fmt"

// CatalogWorld is a monitored world with region from the static catalog.
type CatalogWorld struct {
	Name   string
	Region string
	Slug   string
}

// ListCatalogWorlds flattens region config into worlds for polling.
func ListCatalogWorlds(cfg Config) ([]CatalogWorld, error) {
	if len(cfg.Regions) == 0 {
		return nil, fmt.Errorf("ffxivlodestone: config has no regions")
	}
	var out []CatalogWorld
	for region, rc := range cfg.Regions {
		for _, w := range rc.Worlds {
			if w.Name == "" {
				return nil, fmt.Errorf("ffxivlodestone: empty world name in region %q", region)
			}
			out = append(out, CatalogWorld{
				Name:   w.Name,
				Region: region,
				Slug:   w.Slug,
			})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("ffxivlodestone: no worlds in catalog")
	}
	return out, nil
}
