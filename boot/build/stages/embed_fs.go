// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	dirapi "github.com/wippyai/runtime/api/service/fs/directory"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

var (
	resourcesMu sync.RWMutex
	resources   []wapp.ResourceSpec
)

type embedFSStage struct {
	moduleRoot    string
	embedPatterns []string
}

// EmbedFS creates a stage that collects fs.directory entries for embedding and
// transforms them to fs.embed. moduleRoot is the root that module-relative
// directories resolve against when no module source root is registered in
// context (publish, which loads entries straight from a module checkout); pass
// an empty string when the lock loader has already registered source roots.
func EmbedFS(moduleRoot string, embedPatterns ...string) boot.Stage {
	return &embedFSStage{
		moduleRoot:    moduleRoot,
		embedPatterns: embedPatterns,
	}
}

func (s *embedFSStage) Name() string {
	return "embed_fs"
}

func (s *embedFSStage) Execute(ctx context.Context, entries *[]registry.Entry) error {
	log := logs.GetLogger(ctx)
	setResources(nil)

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

	res, err := collectResources(ctx, s.moduleRoot, filteredEntries, log)
	if err != nil {
		return err
	}

	setResources(res)

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

func setResources(res []wapp.ResourceSpec) {
	resourcesMu.Lock()
	defer resourcesMu.Unlock()
	resources = res
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

func collectResources(ctx context.Context, moduleRoot string, entries []registry.Entry, logger *zap.Logger) ([]wapp.ResourceSpec, error) {
	specs := make([]wapp.ResourceSpec, 0, len(entries))
	for _, entry := range entries {
		if entry.Kind != dirapi.Kind {
			continue
		}

		cfg := directoryConfig(entry)
		if cfg.Directory == "" {
			return nil, fmt.Errorf("embed %s: directory path missing", entry.ID.String())
		}

		dir := resolveEmbedDirectory(ctx, moduleRoot, entry, cfg)

		info, err := os.Stat(dir)
		if err != nil {
			return nil, fmt.Errorf("embed %s: directory %q not found: %w", entry.ID.String(), dir, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("embed %s: path %q is not a directory", entry.ID.String(), dir)
		}

		specs = append(specs, wapp.ResourceSpec{
			ID:   wapp.NewID(entry.ID.NS, entry.ID.Name),
			FS:   os.DirFS(dir),
			Meta: wapp.Metadata(entry.Meta),
		})

		logger.Info("collected directory for embedding",
			zap.String("id", entry.ID.String()),
			zap.String("directory", dir))
	}
	return specs, nil
}

func directoryConfig(entry registry.Entry) *dirapi.Config {
	cfg := &dirapi.Config{}
	if entry.Data == nil {
		return cfg
	}
	data, ok := entry.Data.Data().(map[string]any)
	if !ok {
		return cfg
	}
	if directory, ok := data["directory"].(string); ok {
		cfg.Directory = directory
	}
	if base, ok := data["base"].(string); ok {
		cfg.Base = base
	}
	return cfg
}

func resolveEmbedDirectory(ctx context.Context, moduleRoot string, entry registry.Entry, cfg *dirapi.Config) string {
	dir := dirapi.ResolveDirectory(ctx, entry, cfg)
	if moduleRoot != "" && cfg.Base != dirapi.BaseProject && !dirapi.IsConfiguredPathAbsolute(cfg.Directory) {
		return filepath.Join(moduleRoot, cfg.Directory)
	}
	return dir
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
				Data: payload.New(map[string]any{}),
			}
		} else {
			transformed[i] = entry
		}
	}
	return transformed
}
