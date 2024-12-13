package registry

import (
	"fmt"
	reg "github.com/ponyruntime/pony/api/registry"
	"sort"
	"sync"

	"github.com/ponyruntime/pony/api/payload"
)

type trimmedEntry struct {
	Path reg.Path
	Kind reg.Kind
	Meta reg.Metadata
}

// loader is responsible for loading, parsing, and managing reg entries from payloads.
// It uses a provided payload.Transcoder to unmarshal payload data into structured reg.Entry objects.
// Entries are stored internally and can be accessed in a sorted manner.
// The loader supports optional prefixing for logical grouping of entries, and it's safe for concurrent use.
type loader struct {
	prefix  string
	dtt     payload.Transcoder
	entries map[string]reg.Entry
	mutex   *sync.RWMutex
}

// NewLoader creates a new reg loader with optional replacer support
func NewLoader(dtt payload.Transcoder) reg.Loader {
	return &loader{
		dtt:     dtt,
		entries: make(map[string]reg.Entry),
		mutex:   &sync.RWMutex{},
	}
}

// Register processes the payloads and extracts configuration entries.
func (l *loader) Register(path reg.Path, payloads ...payload.Payload) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	for _, p := range payloads {
		var entry trimmedEntry
		err := l.dtt.Unmarshal(p, &entry)
		if err != nil {
			return fmt.Errorf("failed to unmarshal payload as reg.Entry: %w", err)
		}

		if entry.Path == "" {
			return fmt.Errorf("missing Path in reg entry")
		}
		if entry.Kind == "" {
			return fmt.Errorf("missing Kind in reg entry")
		}

		fullID := path + entry.Path
		l.entries[string(fullID)] = reg.Entry{
			Path: fullID,
			Kind: entry.Kind,
			Meta: entry.Meta,
			Data: p,
		}
	}
	return nil
}

// Entries returns a sorted list of loaded entries.
func (l *loader) Entries() []reg.Entry {
	l.mutex.RLock()
	defer l.mutex.RUnlock()

	sortedEntries := make([]reg.Entry, 0, len(l.entries))
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
	l.entries = make(map[string]reg.Entry)
}
