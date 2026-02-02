package stages

import (
	"context"
	"os"
	"sync"

	"github.com/wippyai/wapp"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	dirapi "github.com/wippyai/runtime/api/service/fs/directory"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	"go.uber.org/zap"
)

var (
	resourcesMu sync.RWMutex
	resources   []wapp.ResourceSpec
)

type embedFSStage struct {
	embedPatterns []string
}

// EmbedFS creates a stage that collects fs.directory entries for embedding.
// It transforms these entries to fs.embed and stores resource specs in context.
func EmbedFS(embedPatterns ...string) boot.Stage {
	return &embedFSStage{
		embedPatterns: embedPatterns,
	}
}

func (s *embedFSStage) Name() string {
	return "embed_fs"
}

func (s *embedFSStage) Execute(ctx context.Context, entries *[]registry.Entry) error {
	log := logs.GetLogger(ctx)

	embeddableIDs := filterEmbeddableEntries(*entries, s.embedPatterns)
	if len(embeddableIDs) == 0 {
		log.Info("no directories to embed")
		return nil
	}

	log.Info("collecting directories for embedding", zap.Int("count", len(embeddableIDs)))

	embeddableMap := make(map[string]bool)
	for _, id := range embeddableIDs {
		embeddableMap[id.String()] = true
	}

	var filteredEntries []registry.Entry
	for _, entry := range *entries {
		if embeddableMap[entry.ID.String()] {
			filteredEntries = append(filteredEntries, entry)
		}
	}

	res, err := collectResources(filteredEntries, log)
	if err != nil {
		return err
	}

	resourcesMu.Lock()
	resources = res
	resourcesMu.Unlock()

	transformed := transformEntries(*entries, embeddableIDs)
	*entries = transformed

	log.Info("transformed entries for embedding",
		zap.Int("embedded", len(embeddableIDs)),
		zap.Int("resources", len(res)))

	return nil
}

// GetResources retrieves collected resources.
func GetResources(_ context.Context) []wapp.ResourceSpec {
	resourcesMu.RLock()
	defer resourcesMu.RUnlock()
	return resources
}

func filterEmbeddableEntries(entries []registry.Entry, embedPatterns []string) []registry.ID {
	var embeddable []registry.ID
	for _, entry := range entries {
		if entry.Kind != dirapi.Kind {
			continue
		}
		if len(embedPatterns) == 0 {
			embeddable = append(embeddable, entry.ID)
			continue
		}
		for _, pattern := range embedPatterns {
			if entry.ID.String() == pattern || entry.ID.Name == pattern {
				embeddable = append(embeddable, entry.ID)
				break
			}
		}
	}
	return embeddable
}

func collectResources(entries []registry.Entry, logger *zap.Logger) ([]wapp.ResourceSpec, error) {
	specs := make([]wapp.ResourceSpec, 0, len(entries))
	for _, entry := range entries {
		if entry.Kind != dirapi.Kind {
			continue
		}

		data := entry.Data.Data()
		cfg, ok := data.(map[string]interface{})
		if !ok {
			logger.Warn("failed to decode directory config, skipping", zap.String("id", entry.ID.String()))
			continue
		}

		directory, ok := cfg["directory"].(string)
		if !ok || directory == "" {
			logger.Warn("directory path missing, skipping", zap.String("id", entry.ID.String()))
			continue
		}

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

		spec := wapp.ResourceSpec{
			ID:   wapp.NewID(entry.ID.NS, entry.ID.Name),
			FS:   os.DirFS(directory),
			Meta: wapp.Metadata(entry.Meta),
		}
		specs = append(specs, spec)

		logger.Info("collected directory for embedding",
			zap.String("id", entry.ID.String()),
			zap.String("directory", directory))
	}
	return specs, nil
}

func transformEntries(entries []registry.Entry, embeddableIDs []registry.ID) []registry.Entry {
	embeddableMap := make(map[string]bool)
	for _, id := range embeddableIDs {
		embeddableMap[id.String()] = true
	}

	transformed := make([]registry.Entry, len(entries))
	for i, entry := range entries {
		if embeddableMap[entry.ID.String()] && entry.Kind == dirapi.Kind {
			transformed[i] = registry.Entry{
				ID:   entry.ID,
				Kind: embedapi.Kind,
				Meta: entry.Meta,
				Data: payload.New(map[string]interface{}{}),
			}
		} else {
			transformed[i] = entry
		}
	}
	return transformed
}
