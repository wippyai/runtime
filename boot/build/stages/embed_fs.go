package stages

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/pack"
	"go.uber.org/zap"
)

var (
	resourcesMu sync.RWMutex
	resources   []pack.ResourceSpec
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

	// Filter which entries should be embedded
	embeddableIDs := pack.FilterEmbeddableEntries(*entries, s.embedPatterns)

	if len(embeddableIDs) == 0 {
		log.Info("no directories to embed")
		return nil
	}

	log.Info("collecting directories for embedding",
		zap.Int("count", len(embeddableIDs)))

	// Create a map for quick lookup
	embeddableMap := make(map[string]bool)
	for _, id := range embeddableIDs {
		embeddableMap[id.String()] = true
	}

	// Filter entries to only those that should be embedded
	var filteredEntries []registry.Entry
	for _, entry := range *entries {
		if embeddableMap[entry.ID.String()] {
			filteredEntries = append(filteredEntries, entry)
		}
	}

	// Collect filesystem resources only for filtered entries
	res, err := pack.CollectResources(filteredEntries, log)
	if err != nil {
		return err
	}

	// Store resources in package variable for retrieval by pack command
	resourcesMu.Lock()
	resources = res
	resourcesMu.Unlock()

	// Transform entries
	transformed := pack.TransformEntries(*entries, embeddableIDs)
	*entries = transformed

	log.Info("transformed entries for embedding",
		zap.Int("embedded", len(embeddableIDs)),
		zap.Int("resources", len(res)))

	return nil
}

// GetResources retrieves collected resources.
func GetResources(_ context.Context) []pack.ResourceSpec {
	resourcesMu.RLock()
	defer resourcesMu.RUnlock()
	return resources
}
