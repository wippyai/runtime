package registry

import (
	"fmt"
	"sort"
	"sync"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

type trimmedEntry struct {
	Path registry.Path
	Kind registry.Kind
	Meta registry.Metadata
}

// loader is responsible for loading, parsing, and managing registry entries from payloads.
// It uses a provided payload.Transcoder to unmarshal payload data into structured registry.Entry objects.
// Entries are stored internally and can be accessed in a sorted manner.
// The loader supports optional prefixing for logical grouping of entries, and it's safe for concurrent use.
type loader struct {
	prefix  string
	dtt     payload.Transcoder
	entries map[string]registry.Entry
	mutex   *sync.RWMutex
}

// NewLoader creates a new registry loader with optional replacer support
func NewLoader(dtt payload.Transcoder) registry.Loader {
	return &loader{
		dtt:     dtt,
		entries: make(map[string]registry.Entry),
		mutex:   &sync.RWMutex{},
	}
}

// WithPrefix sets a prefix for the loader.
func (l *loader) WithPrefix(prefix registry.Path) registry.Loader {
	return &loader{
		prefix:  string(prefix),
		dtt:     l.dtt,
		entries: l.entries,
		mutex:   l.mutex,
	}
}

// Load processes the payloads and extracts configuration entries.
func (l *loader) Load(payloads ...payload.Payload) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	for _, p := range payloads {
		var entry trimmedEntry
		err := l.dtt.Unmarshal(p, &entry)
		if err != nil {
			return fmt.Errorf("failed to unmarshal payload as registry.Entry: %w", err)
		}

		if entry.Path == "" {
			return fmt.Errorf("missing Path in registry entry")
		}
		if entry.Kind == "" {
			return fmt.Errorf("missing Kind in registry entry")
		}

		fullID := l.getFullID(entry.Path)
		l.entries[fullID] = registry.Entry{
			Path: registry.Path(fullID),
			Kind: entry.Kind,
			Meta: entry.Meta,
			Data: p,
		}
	}
	return nil
}

// Entries returns a sorted list of loaded entries.
func (l *loader) Entries() []registry.Entry {
	l.mutex.RLock()
	defer l.mutex.RUnlock()

	sortedEntries := make([]registry.Entry, 0, len(l.entries))
	for _, entry := range l.entries {
		sortedEntries = append(sortedEntries, entry)
	}

	sort.Slice(sortedEntries, func(i, j int) bool {
		return sortedEntries[i].Path < sortedEntries[j].Path
	})

	return sortedEntries
}

// Reset clears all loaded entries.
func (l *loader) Reset() {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.entries = make(map[string]registry.Entry)
}

// getFullID constructs the full Path, including the prefix if set.
func (l *loader) getFullID(id registry.Path) string {
	if l.prefix == "" {
		return string(id)
	}
	return l.prefix + "." + string(id)
}
