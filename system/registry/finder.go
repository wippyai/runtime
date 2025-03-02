package registry

import (
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/metamatch"
)

// memoryFinder implements the Finder interface for in-memory registry state
type memoryFinder struct {
	reg registry.Registry
}

// NewFinder creates a new Finder that can search registry entries
func NewFinder(r registry.Registry) registry.Finder {
	return &memoryFinder{reg: r}
}

// Find retrieves all entries with metadata matching the provided criteria
// and returns them as a slice of entries.
func (f *memoryFinder) Find(meta registry.Metadata) ([]registry.Entry, error) {
	// Create matcher from the metadata
	matcher := metadataToMatcher(meta)

	entries, err := f.reg.GetAllEntries()
	if err != nil {
		return nil, err
	}

	// Filter entries based on matcher
	var result []registry.Entry
	for _, entry := range entries {
		// todo: add kind match, consts?
		if meta["kind"] != nil && entry.Kind != meta["kind"].(string) {
			continue
		}

		if matcher.Match(entry.Meta) {
			result = append(result, entry)
		}
	}

	return result, nil
}

// metadataToMatcher converts registry metadata to a metamatch.Matcher
func metadataToMatcher(metadata registry.Metadata) *metamatch.Matcher {
	matcher := metamatch.NewMatcher()

	// Add conditions for each metadata entry
	for key, value := range metadata {
		switch v := value.(type) {
		case string:
			matcher = matcher.WithStringValue(key, v)
		case bool:
			matcher = matcher.WithBoolValue(key, v)
		case int:
			matcher = matcher.WithIntValue(key, v)
		case []string:
			// For string arrays, we assume all values must be present (AND logic)
			for _, tag := range v {
				matcher = matcher.WithTagContains(key, tag)
			}
		default:
			// For other types, use exact value matching
			matcher = matcher.WithExactValue(key, value)
		}
	}

	return matcher
}
