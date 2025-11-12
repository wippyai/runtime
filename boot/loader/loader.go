package loader

import (
	"context"
	"fmt"
	iofs "io/fs"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/boot/loader/interpolate"
	"go.uber.org/zap"
)

// Loader manages loading of registry entries
type Loader struct {
	fileLoader   *FileLoader
	dtt          payload.Transcoder
	log          *zap.Logger
	interpolator *interpolate.Helper
	exports      map[string]Export
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
		exports:      make(map[string]Export),
	}
}

// LoadFS loads all entries from FS.
func (l *Loader) LoadFS(ctx context.Context, fs iofs.FS) ([]registry.Entry, error) {
	payloads, err := l.fileLoader.LoadFS(fs)
	if err != nil {
		return nil, fmt.Errorf("failed to load files: %w", err)
	}

	var entries []registry.Entry
	for _, p := range payloads {
		fileEntries, err := l.processFile(ctx, fs, p)
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
func (l *Loader) LoadDir(ctx context.Context, fs iofs.FS, dirPath string) ([]registry.Entry, error) {
	payloads, err := l.fileLoader.LoadDir(fs, dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load files from directory %s: %w", dirPath, err)
	}

	var entries []registry.Entry
	for _, p := range payloads {
		fileEntries, err := l.processFile(ctx, fs, p)
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
func (l *Loader) LoadFile(ctx context.Context, fs iofs.FS, filePath string) ([]registry.Entry, error) {
	filePayload, err := l.fileLoader.LoadFile(fs, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load file %s: %w", filePath, err)
	}

	entries, err := l.processFile(ctx, fs, filePayload)
	if err != nil {
		return nil, fmt.Errorf("failed to process file %s: %w", filePath, err)
	}

	return entries, nil
}

// processFile processes a single file and returns registry entries
func (l *Loader) processFile(ctx context.Context, fSys iofs.FS, p *FilePayload) ([]registry.Entry, error) {
	// Interpolate values
	interpolated, err := l.interpolator.Interpolate(p, interpolate.EntryContext{
		Filename: p.Source(),
		FS:       fSys,
		Context:  ctx,
	})
	if err != nil {
		return nil, fmt.Errorf("interpolation failed: %w", err)
	}

	// Extract entries
	newEntries, err := ExtractDependenciesToEntries(interpolated, l.dtt)
	if err != nil {
		return nil, fmt.Errorf("extract entries: %w", err)
	}

	// Validate entries
	for _, entry := range newEntries {
		if err := validateEntry(entry); err != nil {
			return nil, fmt.Errorf("invalid entry in %s: %w", p.Source(), err)
		}
	}

	return newEntries, nil
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
