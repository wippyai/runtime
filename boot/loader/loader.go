package loader

import (
	"context"
	iofs "io/fs"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/loader/interpolate"
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
		return nil, NewLoadFilesError(err)
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
		return nil, NewLoadDirectoryFilesError(dirPath, err)
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
		return nil, NewLoadFileError(filePath, err)
	}

	entries, err := l.processFile(ctx, fs, filePayload)
	if err != nil {
		return nil, NewProcessFileError(filePath, err)
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
		return nil, NewInterpolationError(err)
	}

	// Extract entries
	newEntries, err := ExtractDependenciesToEntries(interpolated, l.dtt)
	if err != nil {
		return nil, NewExtractEntriesError(err)
	}

	// Validate entries
	for _, entry := range newEntries {
		if err := validateEntry(entry); err != nil {
			return nil, NewInvalidEntryError(p.Source(), err)
		}
	}

	return newEntries, nil
}

func validateEntry(entry registry.Entry) error {
	if entry.ID.NS == "" {
		return ErrMissingNamespace
	}
	if entry.ID.Name == "" {
		return ErrMissingName
	}
	if entry.Kind == "" {
		return ErrMissingKind
	}
	return nil
}
