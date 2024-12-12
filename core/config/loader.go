package config

import (
	"fmt"
	"github.com/ponyruntime/pony/internal/gostruct"
	"sort"
	"strings"
	"sync"

	"github.com/ponyruntime/pony/api/config"
	"github.com/ponyruntime/pony/api/payload"
)

type trimmedEntry struct {
	Path config.Path
	Kind config.Kind
	Meta config.Metadata
}

type FileReader interface {
	ReadFile(path string) ([]byte, error)
}

type Variables map[string]string

type Option func(*loader)

// WithFileReader enables file:// loading support
func WithFileReader(fs FileReader) Option {
	return func(l *loader) {
		l.fileReader = fs
	}
}

// WithVariables enables ${var} interpolation support
func WithVariables(vars Variables) Option {
	return func(l *loader) {
		l.variables = vars
	}
}

type loader struct {
	prefix  string
	dtt     payload.Transcoder
	entries map[string]config.Entry
	mutex   *sync.RWMutex
	// Optional replacers
	fileReader FileReader
	variables  Variables
	replacer   *gostruct.StringReplacer
}

// NewLoader creates a new config loader with optional replacer support
func NewLoader(dtt payload.Transcoder, opts ...Option) config.Loader {
	l := &loader{
		dtt:     dtt,
		entries: make(map[string]config.Entry),
		mutex:   &sync.RWMutex{},
	}

	// Apply options
	for _, opt := range opts {
		opt(l)
	}

	// Initialize replacer if needed
	if l.fileReader != nil || l.variables != nil {
		l.replacer = gostruct.NewStringReplacer(l.replaceString)
	}

	return l
}

// WithPrefix sets a prefix for the loader.
func (l *loader) WithPrefix(prefix config.Path) config.Loader {
	return &loader{
		prefix:     string(prefix),
		dtt:        l.dtt,
		entries:    l.entries,
		mutex:      l.mutex,
		fileReader: l.fileReader,
		variables:  l.variables,
		replacer:   l.replacer,
	}
}

// Load processes the payloads and extracts configuration entries.
func (l *loader) Load(payloads ...payload.Payload) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	for _, p := range payloads {
		// Process replacements if enabled
		p, err2 := l.preparePayload(p)
		if err2 != nil {
			return err2
		}

		var entry trimmedEntry
		err := l.dtt.Unmarshal(p, &entry)
		if err != nil {
			return fmt.Errorf("failed to unmarshal payload as config.Entry: %w", err)
		}

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

func (l *loader) preparePayload(p payload.Payload) (payload.Payload, error) {
	if l.replacer == nil {
		return p, nil
	}

	unpacked := new(interface{})
	err := l.dtt.Unmarshal(p, unpacked)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	processed, err := l.replacer.Replace(unpacked)
	if err != nil {
		return nil, fmt.Errorf("processing replacements: %w", err)
	}

	return payload.New(processed), nil
}

// Entries returns a sorted list of loaded entries.
func (l *loader) Entries() []config.Entry {
	l.mutex.RLock()
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
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.entries = make(map[string]config.Entry)
}

// getFullID constructs the full Path, including the prefix if set.
func (l *loader) getFullID(id config.Path) string {
	if l.prefix == "" {
		return string(id)
	}
	return l.prefix + "." + string(id)
}

func (l *loader) replaceVariables(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}

	result := s
	for k, v := range l.variables {
		placeholder := "${" + k + "}"
		result = strings.ReplaceAll(result, placeholder, v)
	}

	return result
}

// replaceString handles both file:// and ${var} replacements
func (l *loader) replaceString(s string) (string, error) {
	// Handle file:// prefix if reader enabled
	if strings.HasPrefix(s, "file://") && l.fileReader != nil {
		data, err := l.fileReader.ReadFile(strings.TrimPrefix(s, "file://"))
		if err != nil {
			return "", fmt.Errorf("reading file: %w", err)
		}

		return string(data), nil
	}

	// Handle ${var} if variables enabled
	if l.variables != nil {
		return l.replaceVariables(s), nil
	}

	return s, nil
}
