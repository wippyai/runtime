package loader

import (
	"fmt"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

// Export represents a capability that a module/system makes available to dependent modules
type Export struct {
	Name    string            `json:"name" yaml:"name"`
	Targets map[string]string `json:"targets,omitempty" yaml:"targets,omitempty"`
}

// Requirement represents a dependency that a module needs
type Requirement struct {
	Parameter   string              `json:"parameter" yaml:"parameter"`
	Description string              `json:"description,omitempty" yaml:"description,omitempty"`
	Targets     []RequirementTarget `json:"targets" yaml:"targets"`
}

// RequirementTarget defines how a requirement is mapped to an entry
type RequirementTarget struct {
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value" yaml:"value"`
}

type NsRequirementTarget struct {
	Entry string `json:"entry" yaml:"entry"`
	Path  string `json:"path" yaml:"path"`
}

// FileContent represents the structure of a registry configuration file.
// It supports both single entry and batch entries formats, with common
// metadata that can be applied to all entries in a file.
type FileContent struct {
	Version   string            `json:"version,omitempty" yaml:"version,omitempty"`
	Namespace string            `json:"namespace"`
	Meta      registry.Metadata `json:"meta,omitempty" yaml:"meta,omitempty"`

	Requirements []Requirement `json:"requirements,omitempty" yaml:"requirements,omitempty"`

	// Store raw entries as map slice
	RawEntries []map[string]interface{} `json:"entries,omitempty" yaml:"entries,omitempty"`

	// Single-entry format fields
	Name string                 `json:"name,omitempty" yaml:"name,omitempty"`
	Kind string                 `json:"kind,omitempty" yaml:"kind,omitempty"`
	Data map[string]interface{} `json:",inline"`
}

func ExtractDependenciesToEntries(p payload.Payload, dtt payload.Transcoder) ([]registry.Entry, error) {
	var content FileContent
	if err := dtt.Unmarshal(p, &content); err != nil {
		return nil, fmt.Errorf("unmarshal content: %w", err)
	}

	if content.Namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}

	entries := make([]registry.Entry, 0)

	// Process requirements
	for _, requirement := range content.Requirements {
		entries = append(entries, registry.Entry{
			ID: registry.ID{
				NS: content.Namespace,
				//Name: strings.ToLower(requirement.Parameter) + "_requirement",
				Name: requirement.Parameter, // == name
			},
			Kind: registry.KindNamespaceDefinition,
			Meta: registry.Metadata{
				"parameter":   requirement.Parameter,
				"description": requirement.Description,
			},
			Data: payload.New(requirement),
		})
	}

	// Process raw entries (the entries array from YAML)
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

		// Spawn entry payload
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

	// Support single-entry format if no entries array and name/kind are present
	if len(content.RawEntries) == 0 && content.Name != "" && content.Kind != "" {
		// Merge metadata
		mergedMeta := mergeMeta(content.Meta, nil)

		// Build entry data preserving the original structure
		entryMap := map[string]interface{}{
			"namespace": content.Namespace,
			"name":      content.Name,
			"kind":      content.Kind,
		}

		// Since we're using ,inline, the Data field contains all the flattened fields
		// We need to reconstruct the nested data structure
		if content.Data != nil {
			// Create a nested data field with all the custom fields
			nestedData := make(map[string]interface{})
			for k, v := range content.Data {
				// Skip the fields that are part of the struct definition
				if k != "namespace" && k != "name" && k != "kind" && k != "meta" && k != "version" && k != "requirements" && k != "entries" {
					nestedData[k] = v
				}
			}
			if len(nestedData) > 0 {
				entryMap["data"] = nestedData
			}
		}

		entry := registry.Entry{
			ID: registry.ID{
				NS:   content.Namespace,
				Name: content.Name,
			},
			Kind: content.Kind,
			Meta: mergedMeta,
			Data: payload.New(entryMap),
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// mergeMeta merges base and override metadata
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
