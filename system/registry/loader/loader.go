package loader

import (
	"fmt"
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

// processFile processes a single file and returns registry entries
func (l *Loader) processFile(p *FilePayload, rootPath string, vars interpolate.Variables) ([]registry.Entry, error) {
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
