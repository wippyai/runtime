package pack

import (
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	dirapi "github.com/wippyai/runtime/api/service/directory"
	embedapi "github.com/wippyai/runtime/api/service/embed"
)

// TransformToEmbed transforms a fs.directory entry to fs.embed entry.
// This removes the directory path and changes the kind.
func TransformToEmbed(entry registry.Entry) registry.Entry {
	if entry.Kind != dirapi.Kind {
		return entry
	}

	// Create new entry with fs.embed kind
	transformed := entry
	transformed.Kind = embedapi.Kind

	// Set empty config for embed entries
	transformed.Data = payload.New(map[string]interface{}{})

	return transformed
}

// TransformEntries transforms all entries in the embeddableIDs list to fs.embed.
// Returns the transformed entries slice.
func TransformEntries(entries []registry.Entry, embeddableIDs []registry.ID) []registry.Entry {
	// Create ID lookup map
	embeddableMap := make(map[string]bool)
	for _, id := range embeddableIDs {
		embeddableMap[id.String()] = true
	}

	// Transform matching entries
	transformed := make([]registry.Entry, len(entries))
	for i, entry := range entries {
		if embeddableMap[entry.ID.String()] {
			transformed[i] = TransformToEmbed(entry)
		} else {
			transformed[i] = entry
		}
	}

	return transformed
}
