package pack

import (
	"io/fs"
	"os"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	dirapi "github.com/wippyai/runtime/api/service/fs/directory"
	"go.uber.org/zap"
)

// ResourceSpec specifies a filesystem resource to be packed.
type ResourceSpec struct {
	ID   registry.ID // Resource ID (matches entry ID)
	FS   fs.FS       // Filesystem to pack
	Meta attrs.Bag   // Resource metadata
}

// CollectResources scans entries for fs.directory entries and collects their filesystems.
// Returns specs for all embeddable directories found.
func CollectResources(entries []registry.Entry, logger *zap.Logger) ([]ResourceSpec, error) {
	specs := make([]ResourceSpec, 0, len(entries))

	for _, entry := range entries {
		if entry.Kind != dirapi.Kind {
			continue
		}

		// Extract directory config
		data := entry.Data.Data()
		cfg, ok := data.(map[string]interface{})
		if !ok {
			logger.Warn("failed to decode directory config, skipping",
				zap.String("id", entry.ID.String()))
			continue
		}

		directory, ok := cfg["directory"].(string)
		if !ok || directory == "" {
			logger.Warn("directory path missing, skipping",
				zap.String("id", entry.ID.String()))
			continue
		}

		// Check if directory exists
		info, err := os.Stat(directory)
		if err != nil {
			logger.Warn("directory not found, skipping",
				zap.String("id", entry.ID.String()),
				zap.String("directory", directory),
				zap.Error(err))
			continue
		}

		if !info.IsDir() {
			logger.Warn("path is not a directory, skipping",
				zap.String("id", entry.ID.String()),
				zap.String("directory", directory))
			continue
		}

		// Create fs.FS from directory
		fsys := os.DirFS(directory)

		// Create resource spec
		spec := ResourceSpec{
			ID:   entry.ID,
			FS:   fsys,
			Meta: entry.Meta,
		}

		specs = append(specs, spec)

		logger.Info("collected directory for embedding",
			zap.String("id", entry.ID.String()),
			zap.String("directory", directory))
	}

	return specs, nil
}

// FilterEmbeddableEntries returns entry IDs that should be embedded.
// This can be customized to filter based on metadata, patterns, etc.
func FilterEmbeddableEntries(entries []registry.Entry, embedPatterns []string) []registry.ID {
	var embeddable []registry.ID

	for _, entry := range entries {
		if entry.Kind != dirapi.Kind {
			continue
		}

		// If no patterns specified, embed all directories
		if len(embedPatterns) == 0 {
			embeddable = append(embeddable, entry.ID)
			continue
		}

		// Check if entry matches any embed pattern
		for _, pattern := range embedPatterns {
			// Simple string matching for now
			// Can be enhanced with glob patterns
			if entry.ID.String() == pattern || entry.ID.Name == pattern {
				embeddable = append(embeddable, entry.ID)
				break
			}
		}
	}

	return embeddable
}
