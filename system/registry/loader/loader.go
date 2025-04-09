package loader

import (
	"fmt"
	iofs "io/fs"

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

// NewLoader creates a new loader instance
func NewLoader(dtt payload.Transcoder, log *zap.Logger, interpolator *interpolate.Helper) *Loader {
	if log == nil {
		log = zap.NewNop()
	}

	return &Loader{
		fileLoader:   NewFileLoader(log),
		dtt:          dtt,
		log:          log,
		interpolator: interpolator,
	}
}

// LoadFS loads all entries from FS.
func (l *Loader) LoadFS(fs iofs.FS, vars interpolate.Variables) ([]registry.Entry, error) {
	payloads, err := l.fileLoader.LoadFS(fs)
	if err != nil {
		return nil, fmt.Errorf("failed to load files: %w", err)
	}

	var entries []registry.Entry
	for _, p := range payloads {
		fileEntries, err := l.processFile(fs, p, vars)
		if err != nil {
			// Log warning instead of returning error
			l.log.Warn("process file", zap.String("path", p.Source()), zap.Error(err))
			continue
		}
		entries = append(entries, fileEntries...)
	}

	return entries, nil
}

// LoadDir loads all entries from a directory
func (l *Loader) LoadDir(fs iofs.FS, dirPath string, vars interpolate.Variables) ([]registry.Entry, error) {
	payloads, err := l.fileLoader.LoadDir(fs, dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load files from directory %s: %w", dirPath, err)
	}

	var entries []registry.Entry
	for _, p := range payloads {
		fileEntries, err := l.processFile(fs, p, vars)
		if err != nil {
			// Log warning instead of returning error
			l.log.Warn("process file", zap.String("path", p.Source()), zap.Error(err))
			continue
		}
		entries = append(entries, fileEntries...)
	}

	return entries, nil
}

// LoadFile loads entries from a single file
func (l *Loader) LoadFile(fs iofs.FS, filePath string, vars interpolate.Variables) ([]registry.Entry, error) {
	filePayload, err := l.fileLoader.LoadFile(fs, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load file %s: %w", filePath, err)
	}

	entries, err := l.processFile(fs, filePayload, vars)
	if err != nil {
		return nil, fmt.Errorf("failed to process file %s: %w", filePath, err)
	}

	return entries, nil
}

// processFile processes a single file and returns registry entries
func (l *Loader) processFile(fSys iofs.FS, p *FilePayload, vars interpolate.Variables) ([]registry.Entry, error) {
	// Interpolate values
	interpolated, err := l.interpolator.Interpolate(p, interpolate.EntryContext{
		Vars:     vars,
		Filename: p.Source(),
		FS:       fSys,
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
