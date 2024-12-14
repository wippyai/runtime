package loader

import (
	"fmt"
	"github.com/ponyruntime/pony/internal/interpolator"
	"path/filepath"
	"strings"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"
)

// Variables is a map of key-value pairs for variable interpolation
type Variables map[string]string

// FolderLoader manages the loading of registry entries from a directory.
type FolderLoader struct {
	rootPath  string
	namespace string
	dtt       payload.Transcoder
	log       *zap.Logger
	i11p      *interpolator.Interpolator
}

// FileEntry is an internal struct used for unmarshalling entry data.
type FileEntry struct {
	Name string            `json:"name" yaml:"name"`
	Kind registry.Kind     `json:"kind" yaml:"kind"`
	Meta registry.Metadata `json:"meta" yaml:"meta"`
}

// NewFolderLoader creates a new FolderLoader.
func NewFolderLoader(dtt payload.Transcoder, log *zap.Logger) *FolderLoader {
	l := &FolderLoader{
		dtt:  dtt,
		log:  log,
		i11p: interpolator.NewInterpolator(LoadFile, LoadVars),
	}

	if l.log == nil {
		l.log = zap.NewNop()
	}

	return l
}

// Load scans the root directory, loads all supported files, and returns a slice of entries
func (l *FolderLoader) Load(rootPath string, namespace string, vars Variables) ([]registry.Entry, error) {
	l.namespace = namespace
	l.rootPath = rootPath

	entryLoader := NewPayloadLoader(l.log)
	payloads, err := entryLoader.Load(rootPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load entries: %w", err)
	}

	entries := make([]registry.Entry, 0)

	for relPath, p := range payloads {
		l.log.Debug("Processing entry", zap.String("path", relPath))
		entry, err := l.processEntry(relPath, p, vars)
		if err != nil {
			l.log.Error("failed to process entry, skipping", zap.String("path", relPath), zap.Error(err))
			continue
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

func (l *FolderLoader) processEntry(relPath string, p payload.Payload, vars Variables) (registry.Entry, error) {
	interpolatedPayload, err := l.interpolate(p, EntryContext{
		Vars:     vars,
		RootDir:  l.rootPath,
		Filename: filepath.Join(l.rootPath, relPath),
	})

	if err != nil {
		return registry.Entry{}, fmt.Errorf("failed to interpolate payload: %w", err)
	}

	return l.register(interpolatedPayload, relPath)
}

func (l *FolderLoader) interpolate(p payload.Payload, ctx EntryContext) (payload.Payload, error) {
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
func (l *FolderLoader) register(p payload.Payload, relPath string) (registry.Entry, error) {
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

	// Calculate full ID (prefix + entry path)
	fullID := l.calculateFullID(filepath.Dir(relPath), entry.Name)

	l.log.Debug(
		"Registering Entry",
		zap.String("path", string(fullID)),
		zap.String("entryName", entry.Name),
	)

	return registry.Entry{
		Path: fullID,
		Kind: entry.Kind,
		Meta: entry.Meta,
		Data: p,
	}, nil
}

// calculateFullID determines the full registry path based on file path, and entry path. relPath must point to filename.
func (l *FolderLoader) calculateFullID(dirPath string, entryName string) registry.Path {
	// Remove trailing slash if any. we trim at the end
	dirPath = strings.TrimSuffix(dirPath, "/")

	var fullID string
	if dirPath != "" {
		fullID = dirPath + "." + entryName
	} else {
		fullID = entryName
	}

	fullID = strings.ReplaceAll(fullID, "/", ".")
	fullID = strings.ReplaceAll(fullID, "..", ".")
	fullID = strings.TrimPrefix(fullID, ".")

	if l.namespace != "" {
		fullID = l.namespace + ":" + fullID
	}

	return registry.Path(fullID)
}
