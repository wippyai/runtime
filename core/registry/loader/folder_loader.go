package loader

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

// FolderLoader manages the loading of registry entries from a directory.
type FolderLoader struct {
	rootPath     string
	dtt          payload.Transcoder
	entries      map[string]registry.Entry
	mutex        *sync.RWMutex
	entryLoader  *EntryLoader
	interpolator *Interpolator
}

// Option is a function type for configuring the FolderLoader.
type Option func(*FolderLoader)

// WithVariables enables ${var} interpolation support.
func WithVariables(vars Variables) Option {
	return func(l *FolderLoader) {
		//l.interpolator = NewInterpolator(l.rootPath, vars)
	}
}

// NewFolderLoader creates a new FolderLoader.
func NewFolderLoader(dtt payload.Transcoder, opts ...Option) *FolderLoader {
	l := &FolderLoader{
		dtt:     dtt,
		entries: make(map[string]registry.Entry),
		mutex:   &sync.RWMutex{},
	}

	// Apply options
	for _, opt := range opts {
		opt(l)
	}

	return l
}

// Boot scans the root directory, loads all supported files, and registers them as entries.
func (l *FolderLoader) Boot(rootPath string) error {
	//l.mutex.Lock()
	//defer l.mutex.Unlock()
	//
	//l.rootPath = rootPath // Store the root path
	////l.entryLoader = NewEntryLoader(rootPath)
	//if l.interpolator != nil {
	//	//	l.interpolator.rootPath = rootPath
	//}
	//
	////payloads, err := l.entryLoader.Load()
	//if err != nil {
	//	return fmt.Errorf("failed to load entries: %w", err)
	//}
	//
	//for _, p := range payloads {
	//	// Get relative path for prefix calculation
	//	var relPath string
	//	if pathData, ok := p.Data().([]byte); ok {
	//		if path, err := filepath.Rel(l.rootPath, string(pathData)); err == nil {
	//			relPath = path
	//		}
	//	}
	//
	//	prefix := l.calculatePrefix(relPath)
	//
	//	// Interpolate if enabled
	//	if l.interpolator != nil {
	//		p, err = l.interpolator.Interpolate(p, l.dtt)
	//		if err != nil {
	//			return fmt.Errorf("failed to interpolate payload: %w", err)
	//		}
	//	}
	//
	//	if err := l.register(registry.Path(prefix), p); err != nil {
	//		// todo: log instead of error
	//		return fmt.Errorf("failed to register entry: %w", err)
	//	}
	//}

	return nil
}

// register processes the payloads and extracts configuration entries.
func (l *FolderLoader) register(path registry.Path, p payload.Payload) error {
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

	fullID := path + entry.Path // Prefix calculation was moved here
	l.entries[string(fullID)] = registry.Entry{
		Path: fullID,
		Kind: entry.Kind,
		Meta: entry.Meta,
		Data: p,
	}

	return nil
}

// Entries returns a sorted list of all loaded registry entries.
func (l *FolderLoader) Entries() []registry.Entry {
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
func (l *FolderLoader) Reset() {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.entries = make(map[string]registry.Entry)
}

// calculatePrefix determines the registry prefix based on the file's path relative to the root.
func (l *FolderLoader) calculatePrefix(relPath string) string {
	dir := filepath.Dir(relPath)
	if dir == "." {
		return "" // Root directory
	}
	return strings.ReplaceAll(dir, string(os.PathSeparator), ".") + "."
}

// trimmedEntry is an internal struct used for unmarshalling entry data.
type trimmedEntry struct {
	Path registry.Path
	Kind registry.Kind
	Meta registry.Metadata
}
