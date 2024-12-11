package config

import (
	"fmt"
	"sort"
	"sync"

	"github.com/ponyruntime/pony/api/config"
	"github.com/ponyruntime/pony/api/payload"
)

type trimmedEntry struct {
	Path config.Path
	Kind config.Kind
	Meta config.Metadata
}

type loader struct {
	prefix  string
	dtt     payload.Transcoder
	entries map[string]config.Entry
	mutex   *sync.RWMutex // Now a pointer to a mutex
}

// NewLoader creates a new config loader.
func NewLoader(dtt payload.Transcoder) config.Loader {
	return &loader{
		dtt:     dtt,
		entries: make(map[string]config.Entry),
		mutex:   &sync.RWMutex{}, // Allocate a new mutex
	}
}

// WithPrefix sets a prefix for the loader.
func (l *loader) WithPrefix(prefix config.Path) config.Loader {
	return &loader{
		prefix:  string(prefix),
		dtt:     l.dtt,
		entries: l.entries,
		mutex:   l.mutex, // Share the same mutex
	}
}

// Load processes the payloads and extracts configuration entries.
func (l *loader) Load(payloads ...payload.Payload) error {
	l.mutex.Lock() // Acquire write lock
	defer l.mutex.Unlock()

	for _, p := range payloads {
		var entry trimmedEntry
		err := l.dtt.Unmarshal(p, &entry)
		if err != nil {
			return fmt.Errorf("failed to unmarshal payload as config.Entry: %w", err)
		}

		// Validate that Path and Kind are set
		if entry.Path == "" {
			return fmt.Errorf("missing Path in config entry")
		}
		if entry.Kind == "" {
			return fmt.Errorf("missing Kind in config entry")
		}

		fullID := l.getFullID(entry.Path)
		l.entries[fullID] = config.Entry{
			Path:   config.Path(fullID),
			Kind:   entry.Kind,
			Meta:   entry.Meta,
			Config: p,
		}
	}

	return nil
}

// Entries returns a sorted list of loaded entries.
func (l *loader) Entries() []config.Entry {
	l.mutex.RLock() // Acquire read lock
	defer l.mutex.RUnlock()

	sortedEntries := make([]config.Entry, 0, len(l.entries))
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
	l.mutex.Lock() // Acquire write lock
	defer l.mutex.Unlock()

	l.entries = make(map[string]config.Entry) // Create a new empty map
}

// getFullID constructs the full Path, including the prefix if set.
func (l *loader) getFullID(id config.Path) string {
	if l.prefix == "" {
		return string(id)
	}
	return l.prefix + "." + string(id)
}
