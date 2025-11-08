package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/deps"
	transcoder "github.com/ponyruntime/pony/system/payload"
	json2 "github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/lua"
	"github.com/ponyruntime/pony/system/payload/yaml"
	"github.com/ponyruntime/pony/system/registry/loader"
	"github.com/ponyruntime/pony/system/registry/loader/interpolate"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type SerializableEntry struct {
	ID     string                 `json:"id"`
	Kind   string                 `json:"kind"`
	Meta   map[string]interface{} `json:"meta,omitempty"`
	Data   interface{}            `json:"data"`
	Format string                 `json:"format,omitempty"`
}

type SerializableState struct {
	Entries []SerializableEntry `json:"entries"`
}

var stateDumpCmd = &cobra.Command{
	Use:   "state-dump",
	Short: "Dump application state to JSON",
	Long:  "Load all entries and dependencies, serialize raw entries to JSON (pre-resolved state)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger, err := createLogger()
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}

		lockFile, _ := cmd.Flags().GetString("lock-file")
		outputFile, _ := cmd.Flags().GetString("output")
		folderPath := "."

		lockPath, err := deps.FindLockFile(folderPath, lockFile)
		if err != nil {
			return fmt.Errorf("lock file not found: %w", err)
		}

		lockFileObj, err := deps.LoadLockFile(lockPath)
		if err != nil {
			return fmt.Errorf("failed to load lock file: %w", err)
		}

		logger.Info("Loading application state",
			zap.String("src_dir", lockFileObj.Directories.Src),
			zap.String("modules_dir", lockFileObj.Directories.Modules))

		fullLockPath, _ := filepath.Abs(lockPath)
		lockDir := filepath.Dir(fullLockPath)
		appDir := filepath.Join(lockDir, lockFileObj.Directories.Src)

		dtt := transcoder.GlobalTranscoder()
		json2.Register(dtt)
		yaml.Register(dtt)
		lua.Register(dtt)

		folderLoader := loader.NewLoader(dtt, logger, interpolate.NewEntryInterpolator(dtt,
			interpolate.WithInterpolator(interpolate.LoadFile),
		))

		osRoot, err := os.OpenRoot(appDir)
		if err != nil {
			return fmt.Errorf("open folder %s: %w", appDir, err)
		}
		fSys := osRoot.FS()

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancel()

		entries, err := folderLoader.LoadFS(ctx, fSys)
		if err != nil {
			return fmt.Errorf("load entries: %w", err)
		}

		logger.Info("Loaded main application entries", zap.Int("count", len(entries)))

		if len(lockFileObj.Modules) > 0 {
			projectRootFS, err := createProjectRootFS(lockDir)
			if err != nil {
				return fmt.Errorf("create project root filesystem: %w", err)
			}

			loadResult := deps.ConvertFromLockFile(lockFileObj, lockPath)
			parentDependencyMap := deps.CreateParentDependencyMap(entries, loadResult, logger)

			if err := deps.ValidateParentDependencyConflicts(parentDependencyMap, logger); err != nil {
				return fmt.Errorf("parent dependency conflicts: %w", err)
			}

			dependencyEntries, err := loadEntriesFromLoadedModules(ctx, folderLoader, loadResult, projectRootFS, logger, parentDependencyMap)
			if err != nil {
				return fmt.Errorf("load dependencies: %w", err)
			}

			logger.Info("Loaded dependency entries", zap.Int("count", len(dependencyEntries)))
			entries = append(entries, dependencyEntries...)
		}

		logger.Info("Total entries loaded", zap.Int("count", len(entries)))

		serializable := SerializableState{
			Entries: make([]SerializableEntry, len(entries)),
		}

		for i, entry := range entries {
			serializable.Entries[i] = convertEntry(entry, dtt, logger)
		}

		stateData, err := json.MarshalIndent(serializable, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal state: %w", err)
		}

		if err := os.WriteFile(outputFile, stateData, 0600); err != nil {
			return fmt.Errorf("write output file: %w", err)
		}

		logger.Info("State dumped successfully",
			zap.String("output", outputFile),
			zap.Int("entries", len(entries)))

		return nil
	},
}

func convertEntry(entry registry.Entry, dtt payload.Transcoder, logger *zap.Logger) SerializableEntry {
	meta := make(map[string]interface{})
	if entry.Meta != nil {
		for k, v := range entry.Meta {
			meta[k] = v
		}
	}

	var data interface{}
	var format string

	if entry.Data != nil {
		format = string(entry.Data.Format())

		if entry.Data.Format() == payload.Golang {
			data = entry.Data.Data()
		} else {
			transcoded, err := dtt.Transcode(entry.Data, payload.Golang)
			if err != nil {
				logger.Warn("failed to transcode entry data",
					zap.String("entry", entry.ID.String()),
					zap.String("format", string(entry.Data.Format())),
					zap.Error(err))
				data = entry.Data.Data()
			} else {
				data = transcoded.Data()
			}
		}
	}

	return SerializableEntry{
		ID:     entry.ID.String(),
		Kind:   entry.Kind,
		Meta:   meta,
		Data:   data,
		Format: format,
	}
}

func init() {
	rootCmd.AddCommand(stateDumpCmd)

	stateDumpCmd.Flags().StringP("lock-file", "l", "wippy.lock", "path to lock file")
	stateDumpCmd.Flags().StringP("output", "o", "state.json", "output file path")
}

func createProjectRootFS(lockDir string) (fs.FS, error) {
	osRoot, err := os.OpenRoot(lockDir)
	if err != nil {
		return nil, fmt.Errorf("open project root %s: %w", lockDir, err)
	}
	return osRoot.FS(), nil
}

func loadEntriesFromLoadedModules(
	ctx context.Context,
	folderLoader *loader.Loader,
	loadResult *deps.LoadResult,
	rootFS fs.FS,
	logger *zap.Logger,
	parentDependencyMap map[string][]deps.ParentDependencyInfo,
) ([]registry.Entry, error) {
	if loadResult == nil || len(loadResult.Modules) == 0 {
		return nil, nil
	}

	var allEntries []registry.Entry

	for _, module := range loadResult.Modules {
		modulePath := filepath.ToSlash(module.Path)
		moduleFS, err := fs.Sub(rootFS, modulePath)
		if err != nil {
			return nil, fmt.Errorf("create sub-filesystem for module %s: %w", module.Path, err)
		}

		moduleEntries, err := folderLoader.LoadFS(ctx, moduleFS)
		if err != nil {
			return nil, fmt.Errorf("load entries from module %s: %w", module.Path, err)
		}

		moduleName := module.Name.String()
		if parentDependencies, exists := parentDependencyMap[moduleName]; exists {
			for i := range moduleEntries {
				if moduleEntries[i].Kind == registry.KindNamespaceRequirement {
					bestParentID := deps.SelectBestParentDependency(moduleEntries[i], parentDependencies, logger)
					if bestParentID != "" {
						if moduleEntries[i].Meta == nil {
							moduleEntries[i].Meta = make(registry.Metadata)
						}
						moduleEntries[i].Meta["parent"] = bestParentID
						logger.Debug("Set meta.parent for ns.requirement in dump",
							zap.String("requirement_id", moduleEntries[i].ID.String()),
							zap.String("parent_dependency_id", bestParentID),
							zap.String("module_name", moduleName))
					}
				}
			}
		}

		allEntries = append(allEntries, moduleEntries...)
	}

	return allEntries, nil
}
