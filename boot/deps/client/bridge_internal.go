package client

import (
	"fmt"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/graph"
)

// extractDependenciesFromEntries extracts dependencies from registry entries.
// HACK: This is a temporary workaround until the manifest API provides proper dependency information.
// It parses entries looking for ns.dependency kind, extracts component and version fields.
// This duplicates logic from yaml_parser.go but works with parsed entries instead of raw YAML.
//
// TODO: Remove this when manifest API is implemented.
func extractDependenciesFromEntries(entries []registry.Entry, dtt payload.Transcoder) ([]graph.ManifestDependency, error) {
	var deps []graph.ManifestDependency

	for _, entry := range entries {
		if entry.Kind != "ns.dependency" {
			continue
		}

		var entryData struct {
			Component string         `json:"component"`
			Version   string         `json:"version"`
			Params    map[string]any `json:"params"`
		}

		if err := dtt.Unmarshal(entry.Data, &entryData); err != nil {
			return nil, fmt.Errorf("unmarshal dependency entry %s: %w", entry.ID, err)
		}

		if entryData.Component == "" {
			continue
		}

		name, err := graph.ParseName(entryData.Component)
		if err != nil {
			continue
		}

		dep := graph.ManifestDependency{
			Name:       name,
			Version:    entryData.Version,
			Parameters: entryData.Params,
		}

		deps = append(deps, dep)
	}

	return deps, nil
}
