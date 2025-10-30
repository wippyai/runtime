package deps

import (
	"context"
	"path/filepath"

	regapi "github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"
)

// EntryLoader implements ManifestLoader using registry entries.
type EntryLoader struct {
	entries      []regapi.Entry
	replacements map[string]string // module name -> local path
	lockFilePath string
	logger       *zap.Logger
}

// NewEntryLoader creates a new EntryLoader.
func NewEntryLoader(entries []regapi.Entry, logger *zap.Logger) *EntryLoader {
	return &EntryLoader{
		entries:      entries,
		replacements: make(map[string]string),
		logger:       logger,
	}
}

// NewEntryLoaderWithReplacements creates a new EntryLoader with replacements support.
// Replacements allow local paths to be used instead of downloading remote modules.
func NewEntryLoaderWithReplacements(entries []regapi.Entry, replacements []Replacement, lockFilePath string, logger *zap.Logger) *EntryLoader {
	replacementMap := make(map[string]string, len(replacements))
	for _, r := range replacements {
		replacementMap[r.From] = r.To
	}

	return &EntryLoader{
		entries:      entries,
		replacements: replacementMap,
		lockFilePath: lockFilePath,
		logger:       logger,
	}
}

// LoadManifest loads the manifest from registry dependency entries.
func (r *EntryLoader) LoadManifest(_ context.Context) (*Manifest, error) {
	// Find all dependency.component entries
	var entries []regapi.Entry
	for _, entry := range r.entries {
		if entry.Kind == regapi.KindNamespaceDependency {
			entries = append(entries, entry)
		}
	}

	r.logger.Debug("Found dependency entries",
		zap.Int("total_entries", len(r.entries)),
		zap.Int("dependency_entries", len(entries)),
		zap.String("dependency_kind", regapi.KindNamespaceDependency))

	// Convert registry entries to ManifestDependency format
	dependencies := make([]ManifestDependency, 0, len(entries))

	for _, entry := range entries {
		// Extract name and version from entry data
		entryData, ok := entry.Data.Data().(map[string]any)
		if !ok {
			r.logger.Warn("invalid entry data format", zap.String("entry_id", entry.ID.Name))
			continue
		}

		componentValue, nameExists := entryData["component"].(string)
		versionValue, versionExists := entryData["version"].(string)

		if !nameExists || !versionExists {
			r.logger.Warn("missing required fields in dependency entry",
				zap.String("entry_id", entry.ID.Name))
			continue
		}

		var dependencyName Name
		if err := dependencyName.SetName(componentValue); err != nil {
			r.logger.Warn("can't set dependency name in dependency entry", zap.String("entry_id", entry.ID.Name))
			continue
		}

		dependency := ManifestDependency{
			Name:    dependencyName,
			Version: versionValue,
		}

		// Check if this dependency has a replacement (local path)
		if replacementPath, hasReplacement := r.replacements[componentValue]; hasReplacement {
			// Resolve the path relative to the lock file location
			resolvedPath := replacementPath
			if r.lockFilePath != "" && !filepath.IsAbs(replacementPath) {
				resolvedPath = filepath.Join(filepath.Dir(r.lockFilePath), replacementPath)
			}
			dependency.Path = resolvedPath

			r.logger.Debug("Using replacement for dependency",
				zap.String("module", componentValue),
				zap.String("path", resolvedPath))
		}

		dependencies = append(dependencies, dependency)
	}

	return &Manifest{
		Dependencies: dependencies,
	}, nil
}
