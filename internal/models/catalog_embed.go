package models

import (
	"encoding/json"

	rootmanifests "github.com/yeixio/toskar-core/manifests"
)

// NewCatalogEmbedded loads the embedded catalog from repo manifests.
func NewCatalogEmbedded() (*Catalog, error) {
	var manifest CatalogManifest
	if err := json.Unmarshal(rootmanifests.CatalogJSON, &manifest); err != nil {
		return nil, err
	}
	c := &Catalog{
		path:    "embedded:catalog.json",
		entries: make(map[string]CatalogEntry, len(manifest.Models)),
	}
	for _, m := range manifest.Models {
		c.entries[m.ID] = m
	}
	return c, nil
}

// LoadPresetsEmbedded loads embedded presets.
func LoadPresetsEmbedded() ([]PurposePreset, error) {
	var manifest PresetsManifest
	if err := json.Unmarshal(rootmanifests.PresetsJSON, &manifest); err != nil {
		return nil, err
	}
	return manifest.Purposes, nil
}
