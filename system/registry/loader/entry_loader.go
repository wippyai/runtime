package loader

import (
	"fmt"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

// Export represents a capability that a module/system makes available to dependent modules
type Export struct {
	Name string `json:"name" yaml:"name"`
	//Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	//Value       string   `json:"value" yaml:"value"`
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

	//Exports      []Export      `json:"exports,omitempty" yaml:"exports,omitempty"`
	Requirements []Requirement `json:"requirements,omitempty" yaml:"requirements,omitempty"`

	// Store raw entries as map slice
	RawEntries []map[string]interface{} `json:"entries,omitempty" yaml:"entries,omitempty"`

	// Single-entry format fields
	Name string                 `json:"name,omitempty" yaml:"name,omitempty"`
	Kind string                 `json:"kind,omitempty" yaml:"kind,omitempty"`
	Data map[string]interface{} `json:",inline"`
}

type requirementExport struct {
	Requirement
	//ExportValue string
	EntryName string
	Path      string
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

//
//// ExtractEntries parses a payload containing registry entries using the provided
//// transcoder. It handles both single-entry and batch-entry formats, and ensures
//// that metadata is properly merged between file-level and entry-level definitions.
//// Returns a slice of registry entries or an error if parsing fails.
//func ExtractEntries(p payload.Payload, dtt payload.Transcoder, exports map[string]Export) ([]registry.Entry, map[string]Export, error) {
//	var content FileContent
//	if err := dtt.Unmarshal(p, &content); err != nil {
//		return nil, nil, fmt.Errorf("unmarshal content: %w", err)
//	}
//
//	if content.Namespace == "" {
//		return nil, nil, fmt.Errorf("namespace is required")
//	}
//
//	newExports := make(map[string]Export, len(content.Exports))
//	entries := make([]registry.Entry, 0, len(content.RawEntries)+len(content.Exports))
//	for _, export := range content.Exports {
//		entries = append(entries, registry.Entry{
//			ID: registry.ID{
//				NS:   content.Namespace,
//				Name: strings.ToLower(export.Name) + "_export",
//			},
//			Kind: registry.KindNamespaceDefinition,
//			Meta: registry.Metadata{
//				"description": export.Description,
//				"name":        export.Name,
//				"value":       export.Value,
//				"targets":     export.Targets,
//			},
//			Data: payload.New(export),
//		})
//		newExports[export.Name] = export
//	}
//
//	requirementExports := make([]requirementExport, 0, len(content.Requirements))
//	for _, requirement := range content.Requirements {
//		export, ok := exports[requirement.Parameter]
//		if !ok {
//			return nil, nil, fmt.Errorf("requirement is not satisfied: %s", requirement.Parameter)
//		}
//
//		if len(export.Targets) != 0 && !slices.Contains(export.Targets, content.Namespace) {
//			return nil, nil, fmt.Errorf(
//				"requirement %s satisfied, but not exported for the given namespace: %s",
//				requirement.Parameter,
//				content.Namespace,
//			)
//		}
//		requirementExports = append(requirementExports, requirementExport{requirement, export.Value})
//		entries = append(entries, registry.Entry{
//			ID: registry.ID{
//				NS:   content.Namespace,
//				Name: strings.ToLower(requirement.Parameter) + "_requirement",
//			},
//			Kind: registry.KindNamespaceDefinition,
//			Meta: registry.Metadata{
//				"parameter":   requirement.Parameter,
//				"description": requirement.Description,
//			},
//			Data: payload.New(requirement),
//		})
//	}
//
//	// Handle single entry case
//	if content.RawEntries == nil && content.Name != "" && content.Kind != "" {
//		entry := registry.Entry{
//			ID: registry.ID{
//				NS:   content.Namespace,
//				Name: content.Name,
//			},
//			Kind: content.Kind,
//			Meta: content.Meta,
//			Data: p,
//		}
//		return []registry.Entry{entry}, newExports, nil
//	}
//
//	// For batch entries
//	for i, rawEntry := range content.RawEntries {
//		// Validate required fields
//		name, ok := rawEntry["name"].(string)
//		if !ok || name == "" {
//			return nil, nil, fmt.Errorf("entry[%d]: name is required", i)
//		}
//
//		kind, ok := rawEntry["kind"].(string)
//		if !ok || kind == "" {
//			return nil, nil, fmt.Errorf("entry[%d]: kind is required", i)
//		}
//
//		// Convert meta map to registry.Metadata
//		var entryMeta registry.Metadata
//		if metaRaw, ok := rawEntry["meta"]; ok && metaRaw != nil {
//			if metaMap, ok := metaRaw.(map[string]any); ok {
//				entryMeta = metaMap
//			}
//		}
//
//		// Merge metadata
//		mergedMeta := mergeMeta(content.Meta, entryMeta)
//
//		// Update the raw entry's meta field with merged metadata
//		rawEntry["meta"] = mergedMeta
//
//		filteredRequirements := make([]requirementExport, 0)
//		for _, re := range requirementExports {
//			if slices.ContainsFunc(re.Targets, func(target RequirementTarget) bool {
//				return target.Name == "" || strings.EqualFold(target.Name, name)
//			}) {
//				filteredRequirements = append(filteredRequirements, re)
//			}
//		}
//		applyRequirements(rawEntry, filteredRequirements)
//
//		// Spawn entry payload
//		entryData := payload.New(rawEntry)
//
//		entry := registry.Entry{
//			ID: registry.ID{
//				NS:   content.Namespace,
//				Name: name,
//			},
//			Kind: kind,
//			Meta: mergedMeta,
//			Data: entryData,
//		}
//		entries = append(entries, entry)
//	}
//
//	return entries, newExports, nil
//}
//
//func applyRequirements(rawEntry map[string]any, reqs []requirementExport) {
//	for _, req := range reqs {
//		for _, target := range req.Targets {
//			setNestedValueAtPath(rawEntry, target.Value, req.ExportValue)
//		}
//	}
//}
//
//// setNestedValueAtPath is a wrapper around the core path-setting logic
//// that handles the initial path processing
//func setNestedValueAtPath(obj map[string]any, path string, value string) {
//	// Handle leading dot by removing it
//	path, _ = strings.CutPrefix(path, ".")
//
//	parts := strings.Split(path, ".")
//	setValueAtPathParts(obj, parts, value)
//}
//
//// setValueAtPathParts sets a value at the given path parts
//func setValueAtPathParts(current map[string]any, parts []string, value string) {
//	for i := 0; i < len(parts); i++ {
//		part := parts[i]
//		isLastPart := i == len(parts)-1
//		isArrayPart := strings.HasSuffix(part, "[]")
//		cleanPart := getCleanPartName(part, isArrayPart)
//
//		if isLastPart {
//			handleLastPathPart(current, cleanPart, value, isArrayPart)
//		} else {
//			current = handleIntermediatePathPart(current, cleanPart, isArrayPart)
//		}
//	}
//}
//
//// getCleanPartName removes array notation if present
//func getCleanPartName(part string, isArrayPart bool) string {
//	if isArrayPart {
//		return part[:len(part)-2]
//	}
//	return part
//}
//
//// handleLastPathPart sets the value at the final path segment
//func handleLastPathPart(current map[string]any, cleanPart string, value string, isArrayPart bool) {
//	if isArrayPart {
//		appendToArray(current, cleanPart, value)
//	} else {
//		current[cleanPart] = value
//	}
//}
//
//// appendToArray appends a value to an array at the given key,
//// creating the array if needed or converting non-array values to arrays
//func appendToArray(obj map[string]any, key string, value any) {
//	ensureKeyExists(obj, key, []any{})
//
//	// Get existing array or convert to array
//	var arr []any
//	existing, exists := obj[key]
//	if !exists {
//		arr = []any{}
//	} else if existingArr, ok := existing.([]any); ok {
//		arr = existingArr
//	} else {
//		// Convert existing value to first element in array
//		arr = []any{existing}
//	}
//
//	// Append the new value and update
//	arr = append(arr, value)
//	obj[key] = arr
//}
//
//// handleIntermediatePathPart handles navigation through a non-final path segment
//func handleIntermediatePathPart(current map[string]any, cleanPart string, isArrayPart bool) map[string]any {
//	if isArrayPart {
//		return navigateOrCreateArrayElement(current, cleanPart)
//	}
//
//	return navigateOrCreateObject(current, cleanPart)
//}
//
//// navigateOrCreateArrayElement ensures there's an array at the specified key
//// with at least one object element, and returns the last object in the array
//func navigateOrCreateArrayElement(obj map[string]any, key string) map[string]any {
//	// Create a new array with one empty object if key doesn't exist
//	ensureKeyExists(obj, key, []any{make(map[string]any)})
//
//	// Ensure we have an array at this key
//	arr, ok := obj[key].([]any)
//	if !ok {
//		// Convert to array with original value as first element
//		arr = []any{obj[key]}
//		obj[key] = arr
//	}
//
//	// Ensure array has at least one element
//	if len(arr) == 0 {
//		arr = append(arr, make(map[string]any))
//		obj[key] = arr
//	}
//
//	// Get last element (or create it if needed)
//	lastIndex := len(arr) - 1
//	lastElement, ok := arr[lastIndex].(map[string]any)
//	if !ok {
//		// Convert last element to map if it's not already
//		lastElement = make(map[string]any)
//		arr[lastIndex] = lastElement
//		obj[key] = arr
//	}
//
//	return lastElement
//}
//
//// navigateOrCreateObject ensures there's a map at the specified key and returns it
//func navigateOrCreateObject(obj map[string]any, key string) map[string]any {
//	ensureKeyExists(obj, key, make(map[string]any))
//
//	// Ensure we have a map and return it
//	nested, ok := obj[key].(map[string]any)
//	if !ok {
//		// If it's not a map, replace with a new map
//		nested = make(map[string]any)
//		obj[key] = nested
//	}
//
//	return nested
//}
//
//// ensureKeyExists makes sure a key exists in the object with a default value if it doesn't
//func ensureKeyExists(obj map[string]any, key string, defaultValue any) {
//	if _, exists := obj[key]; !exists {
//		obj[key] = defaultValue
//	}
//}
