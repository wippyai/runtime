package __isolate

import (
	"fmt"
	"github.com/ponyruntime/pony/internal/interpolator"
	"github.com/ponyruntime/pony/system/registry/loader/interpolate"
	"path/filepath"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"
)

// Loader manages the loading of registry entries from a directory.
type Loader struct {
	rootPath  string
	namespace string
	dtt       payload.Transcoder
	log       *zap.Logger
	i11p      *interpolator.Interpolator
}

// FileEntry is an helpers struct used for unmarshalling entry data.
type FileEntry struct {
	NS   string            `json:"ns" yaml:"ns"`
	Name string            `json:"name" yaml:"name"`
	Kind string            `json:"kind" yaml:"kind"`
	Meta registry.Metadata `json:"meta" yaml:"meta"`
}

// NewFolderLoader creates a new Loader.
func NewFolderLoader(dtt payload.Transcoder, log *zap.Logger) *Loader {
	l := &Loader{
		dtt:  dtt,
		log:  log,
		i11p: interpolator.NewInterpolator(interpolate.LoadFile, interpolate.LoadVars),
	}

	if l.log == nil {
		l.log = zap.NewNop()
	}

	return l
}

// Load scans the root directory, loads all supported files, and returns a slice of entries
func (l *Loader) Load(
	rootPath string,
	namespace string,
	vars interpolate.Variables,
) ([]registry.Entry, error) {
	l.namespace = namespace
	l.rootPath = rootPath

	entryLoader := NewFileLoader(l.log)
	payloads, err := entryLoader.Load(rootPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load entries: %w", err)
	}

	entries := make([]registry.Entry, 0)

	for relPath, p := range payloads {
		l.log.Debug("processing entry", zap.String("path", relPath))
		entry, err := l.processEntry(relPath, p, vars)
		if err != nil {
			l.log.Error("failed to process entry, skipping", zap.String("path", relPath), zap.Error(err))
			continue
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

func (l *Loader) processEntry(relPath string, p payload.Payload, vars interpolate.Variables) (registry.Entry, error) {
	interpolatedPayload, err := l.interpolate(p, interpolate.EntryContext{
		Vars:     vars,
		RootDir:  l.rootPath,
		Filename: filepath.Join(l.rootPath, relPath),
	})

	if err != nil {
		return registry.Entry{}, fmt.Errorf("failed to interpolate payload: %w", err)
	}

	return l.register(interpolatedPayload, relPath)
}

func (l *Loader) interpolate(p payload.Payload, ctx interpolate.EntryContext) (payload.Payload, error) {
	var data interface{}
	err := l.dtt.Unmarshal(p, &data)

	if err != nil {
		return p, fmt.Errorf("failed to unmarshal payload for interpolation: %w", err)
	}

	ip, err := l.i11p.Interpolate(data, ctx)
	if err != nil {
		return p, err
	}

	return payload.New(ip), nil
}

// register processes the payloads and extracts configuration entries.
func (l *Loader) register(p payload.Payload, relPath string) (registry.Entry, error) {
	var entry FileEntry
	err := l.dtt.Unmarshal(p, &entry)

	if err != nil {
		return registry.Entry{}, fmt.Errorf("failed to unmarshal payload as registry.Entry: %w", err)
	}

	if entry.Name == "" {
		return registry.Entry{}, fmt.Errorf("missing Name in registry entry")
	}

	if entry.Kind == "" {
		return registry.Entry{}, fmt.Errorf("missing Kind in registry entry")
	}

	l.log.Debug(
		"registering entry",
		zap.String("id", fullID.String()),
		zap.String("name", entry.Name),
	)

	return registry.Entry{
		ID:   fullID,
		Kind: registry.Kind(entry.Kind),
		Meta: entry.Meta,
		Data: p,
	}, nil
}
