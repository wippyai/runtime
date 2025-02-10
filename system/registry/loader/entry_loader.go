package loader

import (
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

type FileContent struct {
	Version   string            `json:"version,omitempty" yaml:"version,omitempty"`
	Namespace string            `json:"namespace"`
	Meta      registry.Metadata `json:"meta,omitempty" yaml:"meta,omitempty"`

	// Store raw entries as map slice
	RawEntries []map[string]interface{} `json:"entries,omitempty" yaml:"entries,omitempty"`

	// Single-entry format fields
	Name string                 `json:"name,omitempty" yaml:"name,omitempty"`
	Kind string                 `json:"kind,omitempty" yaml:"kind,omitempty"`
	Data map[string]interface{} `json:",inline"`
}

func ExtractEntries(p payload.Payload, dtt payload.Transcoder) ([]registry.Entry, error) {
	var content FileContent
	if err := dtt.Unmarshal(p, &content); err != nil {
		return nil, fmt.Errorf("failed to unmarshal content: %w", err)
	}

	if content.Namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}

	// Handle single entry case
	if content.RawEntries == nil && content.Name != "" && content.Kind != "" {
		entry := registry.Entry{
			ID: registry.ID{
				NS:   content.Namespace,
				Name: content.Name,
			},
			Kind: content.Kind,
			Meta: content.Meta,
			Data: p,
		}
		return []registry.Entry{entry}, nil
	}

	// For batch entries
	entries := make([]registry.Entry, 0, len(content.RawEntries))
	for i, rawEntry := range content.RawEntries {
		// Validate required fields
		name, ok := rawEntry["name"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("entry[%d]: name is required", i)
		}

		kind, ok := rawEntry["kind"].(string)
		if !ok || kind == "" {
			return nil, fmt.Errorf("entry[%d]: kind is required", i)
		}

		// Convert meta map to registry.Metadata
		var entryMeta registry.Metadata
		if metaRaw, ok := rawEntry["meta"]; ok && metaRaw != nil {
			if metaMap, ok := metaRaw.(map[string]any); ok {
				entryMeta = metaMap
			}
		}

		// Merge metadata
		mergedMeta := mergeMeta(content.Meta, entryMeta)

		// Update the raw entry's meta field with merged metadata
		rawEntry["meta"] = mergedMeta

		// Create entry payload
		entryData := payload.New(rawEntry)

		entry := registry.Entry{
			ID: registry.ID{
				NS:   content.Namespace,
				Name: name,
			},
			Kind: kind,
			Meta: mergedMeta,
			Data: entryData,
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

func mergeMeta(baseMeta, overrideMeta registry.Metadata) registry.Metadata {
	if baseMeta == nil {
		return overrideMeta
	}
	if overrideMeta == nil {
		return baseMeta
	}

	merged := make(registry.Metadata)

	// Copy base metadata
	for k, v := range baseMeta {
		merged[k] = v
	}

	// Override with override metadata
	for k, v := range overrideMeta {
		merged[k] = v // Simply override any existing values
	}

	return merged
}
