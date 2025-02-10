package loader

import (
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

type FileContent struct {
	Version   string            `json:"version,omitempty" yaml:"version,omitempty"`
	Namespace string            `json:"namespace" yaml:"namespace"` // Required field now
	Meta      registry.Metadata `json:"meta,omitempty" yaml:"meta,omitempty"`
	Entries   []EntryData       `json:"entries,omitempty" yaml:"entries,omitempty"`

	// Single-entry format fields
	Name string                 `json:"name,omitempty" yaml:"name,omitempty"`
	Kind string                 `json:"kind,omitempty" yaml:"kind,omitempty"`
	Data map[string]interface{} `json:",inline" yaml:",inline"`
}

type EntryData struct {
	Name string                 `json:"name" yaml:"name"`
	Kind string                 `json:"kind" yaml:"kind"`
	Meta registry.Metadata      `json:"meta,omitempty" yaml:"meta,omitempty"`
	Data map[string]interface{} `json:",inline" yaml:",inline"`
}

// ExtractEntries processes the payload and returns registry entries.
// Returns error if namespace is empty.
func ExtractEntries(p payload.Payload, dtt payload.Transcoder) ([]registry.Entry, error) {
	var content FileContent
	if err := dtt.Unmarshal(p, &content); err != nil {
		return nil, fmt.Errorf("failed to unmarshal content: %w", err)
	}

	if content.Namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}

	// Handle single entry case
	if content.Entries == nil && content.Name != "" && content.Kind != "" {
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

	// Handle multi-entry case
	entries := make([]registry.Entry, 0, len(content.Entries))
	for _, e := range content.Entries {
		entry := registry.Entry{
			ID: registry.ID{
				NS:   content.Namespace,
				Name: e.Name,
			},
			Kind: e.Kind,
			Meta: mergeMeta(content.Meta, e.Meta),
			Data: payload.New(e.Data),
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
		if isSlice(v) {
			merged[k] = mergeSlices(v, overrideMeta[k])
			continue
		}
		merged[k] = v
	}

	// Merge/override with override metadata
	for k, v := range overrideMeta {
		if _, exists := merged[k]; !exists {
			merged[k] = v
			continue
		}
		if isSlice(v) && isSlice(merged[k]) {
			merged[k] = mergeSlices(merged[k], v)
			continue
		}
		merged[k] = v
	}

	return merged
}

func isSlice(v interface{}) bool {
	switch v.(type) {
	case []string, []any:
		return true
	}
	return false
}

func mergeSlices(base, override interface{}) []string {
	var baseSlice, overrideSlice []string

	switch v := base.(type) {
	case []string:
		baseSlice = v
	case []any:
		baseSlice = convertToStringSlice(v)
	case string:
		baseSlice = []string{v}
	}

	if override != nil {
		switch v := override.(type) {
		case []string:
			overrideSlice = v
		case []any:
			overrideSlice = convertToStringSlice(v)
		case string:
			overrideSlice = []string{v}
		}
	}

	seen := make(map[string]bool)
	merged := make([]string, 0)

	for _, s := range append(baseSlice, overrideSlice...) {
		if !seen[s] {
			seen[s] = true
			merged = append(merged, s)
		}
	}

	return merged
}

func convertToStringSlice(slice []interface{}) []string {
	result := make([]string, 0, len(slice))
	for _, v := range slice {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
