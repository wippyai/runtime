package loader

import (
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/registry/loader/interpolate"
	"go.uber.org/zap"
)

// Loader manages loading of registry entries
type Loader struct {
	fileLoader   *FileLoader
	dtt          payload.Transcoder
	log          *zap.Logger
	interpolator *interpolate.Helper
}

// LoaderOption is a function that configures a Loader
type LoaderOption func(*Loader)

// WithLoaderFS sets a custom filesystem for the Loader's FileLoader
func WithLoaderFS(fsys fs.FS) LoaderOption {
	return func(l *Loader) {
		// Replace the FileLoader with one using the custom filesystem
		l.fileLoader = NewFileLoader(l.log, WithFS(fsys))
	}
}

// NewLoader creates a new loader instance with the given options
func NewLoader(dtt payload.Transcoder, log *zap.Logger, interpolator *interpolate.Helper, opts ...LoaderOption) *Loader {
	if log == nil {
		log = zap.NewNop()
	}

	l := &Loader{
		fileLoader:   NewFileLoader(log),
		dtt:          dtt,
		log:          log,
		interpolator: interpolator,
	}

	// Apply options
	for _, opt := range opts {
		opt(l)
	}

	return l
}

// LoadFolder loads all entries from a folder
func (l *Loader) LoadFolder(folderPath string, vars interpolate.Variables) ([]registry.Entry, error) {
	payloads, err := l.fileLoader.LoadFolder(folderPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load files: %w", err)
	}

	var entries []registry.Entry
	for _, p := range payloads {
		fileEntries, err := l.processFile(p, folderPath, vars)
		if err != nil {
			// Log warning instead of returning error
			l.log.Warn("failed to process file",
				zap.String("path", p.Source()),
				zap.Error(err))
			continue
		}

		entries = append(entries, fileEntries...)
	}

	return entries, nil
}

// LoadFile loads entries from a single file
func (l *Loader) LoadFile(filePath string, vars interpolate.Variables) ([]registry.Entry, error) {
	filePayload, err := l.fileLoader.LoadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load file: %w", err)
	}

	entries, err := l.processFile(filePayload, "", vars)
	if err != nil {
		return nil, fmt.Errorf("failed to process file: %w", err)
	}

	return entries, nil
}

// processFile processes a single file and returns registry entries
func (l *Loader) processFile(p *FilePayload, rootPath string, vars interpolate.Variables) ([]registry.Entry, error) {
	// If rootPath is empty, use the directory of the file
	if rootPath == "" {
		rootPath = filepath.Dir(p.Source())
	}

	// Interpolate values
	interpolated, err := l.interpolator.Interpolate(p, interpolate.EntryContext{
		Vars:     vars,
		RootDir:  rootPath,
		Filename: p.Source(),
	})
	if err != nil {
		return nil, fmt.Errorf("interpolation failed: %w", err)
	}

	// Extract entries
	entries, err := ExtractEntries(interpolated, l.dtt)
	if err != nil {
		return nil, fmt.Errorf("failed to extract entries: %w", err)
	}

	// Validate entries
	for i := range entries {
		if err := validateEntry(entries[i]); err != nil {
			return nil, fmt.Errorf("invalid entry in %s: %w", p.Source(), err)
		}
	}

	return entries, nil
}

func validateEntry(entry registry.Entry) error {
	if entry.ID.NS == "" {
		return fmt.Errorf("missing namespace")
	}
	if entry.ID.Name == "" {
		return fmt.Errorf("missing name")
	}
	if entry.Kind == "" {
		return fmt.Errorf("missing kind")
	}
	return nil
}
