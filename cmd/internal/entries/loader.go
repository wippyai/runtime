// Package entries handles loading registry entries from lock files and managing module installation.
package entries

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"git.spiralscout.com/wippy/wapp"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/build"
	"github.com/wippyai/runtime/boot/build/stages"
	auth "github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
	regtop "github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

// LoadFromLockFile loads application entries from the lock file and applies them to the registry.
// This function executes between the Load and Start phases of the boot process.
// If modules are missing, it auto-installs them before loading entries.
func LoadFromLockFile(ctx context.Context, logger *zap.Logger, verbose bool) error {
	lockFilePath := "wippy.lock"

	lockPath, err := lock.Find(".", lockFilePath)
	if err != nil {
		logger.Info("no lock file found, starting with empty registry")
		return nil
	}

	logger.Info("loading entries from lock file", zap.String("path", lockPath))

	lockObj, err := lock.New(lockPath)
	if err != nil {
		return NewLoadLockFileError(err)
	}

	if err := lock.Validate(lockObj); err != nil {
		return NewInvalidLockFileError(err)
	}

	if err := EnsureModulesInstalled(ctx, lockPath, logger); err != nil {
		return NewEnsureModulesInstalledError(err)
	}

	paths := lockObj.GetLoadPaths()
	logger.Debug("load paths from lock file", zap.Strings("paths", paths))

	entries, err := loadEntriesFromPaths(ctx, paths, logger)
	if err != nil {
		return NewLoadEntriesFromPathsError(err)
	}

	logger.Info("loaded entries", zap.Int("count", len(entries)))

	if err := loadEntriesToRegistry(ctx, entries, logger, verbose); err != nil {
		return err // Already has context from registry
	}

	logger.Info("entries loaded to registry successfully")
	return nil
}

// EnsureModulesInstalled checks if modules from the lock file are installed,
// and auto-installs them if missing using the hub client.
func EnsureModulesInstalled(ctx context.Context, lockPath string, logger *zap.Logger) error {
	lockObj, err := lock.New(lockPath)
	if err != nil {
		return NewLoadLockFileError(err)
	}

	if err := lock.Validate(lockObj); err != nil {
		return NewInvalidLockFileError(err)
	}

	modules := lockObj.GetModules()
	if len(modules) == 0 {
		return nil
	}

	lockDir := filepath.Dir(lockPath)
	vendorPath := filepath.Join(lockDir, lockObj.GetVendorPath())
	logger.Debug("checking modules installation", zap.String("vendor_path", vendorPath))

	// Check which modules need installation
	var missingModules []lock.Module
	for _, mod := range modules {
		name, err := graph.ParseName(mod.Name)
		if err != nil {
			logger.Warn("failed to parse module name", zap.String("module", mod.Name), zap.Error(err))
			continue
		}

		// Check for wapp file (new format)
		wappPath := lock.WappPath(name, mod.Version)
		fullWappPath := filepath.Join(vendorPath, wappPath)
		if _, err := os.Stat(fullWappPath); err == nil {
			continue
		}

		// Check for directory (legacy format)
		modulePath := lock.ModulePath(name, mod.Version)
		fullPath := filepath.Join(vendorPath, modulePath)
		if _, err := os.Stat(fullPath); err == nil {
			continue
		}

		missingModules = append(missingModules, mod)
	}

	if len(missingModules) == 0 {
		logger.Debug("all modules already installed")
		return nil
	}

	logger.Info("auto-installing missing modules", zap.Int("count", len(missingModules)))

	// Create hub client
	hubClient, err := createHubClient()
	if err != nil {
		return fmt.Errorf("failed to create hub client: %w", err)
	}

	for _, mod := range missingModules {
		name, err := graph.ParseName(mod.Name)
		if err != nil {
			logger.Warn("failed to parse module name", zap.String("module", mod.Name), zap.Error(err))
			continue
		}

		logger.Info("downloading module",
			zap.String("module", mod.Name),
			zap.String("version", mod.Version))

		// Get download URL from hub
		downloadInfo, err := hubClient.GetDownloadURL(ctx, &hub.DownloadParams{
			Org:     name.Organization,
			Module:  name.Module,
			Version: mod.Version,
		})
		if err != nil {
			return NewDownloadModuleError(mod.Name, err)
		}

		if downloadInfo.URL == "" {
			return ErrNoContentDownloaded
		}

		// Download .wapp file
		wappPath := lock.WappPath(name, mod.Version)
		fullWappPath := filepath.Join(vendorPath, wappPath)

		if err := hubClient.DownloadToFile(ctx, downloadInfo.URL, fullWappPath); err != nil {
			return NewDownloadModuleError(mod.Name, err)
		}
	}

	logger.Info("modules installed successfully")
	return nil
}

// createHubClient creates a hub client using stored credentials.
func createHubClient() (*hub.Client, error) {
	projectDir, _ := os.Getwd()
	authCfg := auth.NewConfig(projectDir)
	store := auth.NewStore(authCfg)

	registryURL := store.DefaultRegistry()

	cred, _ := store.Get(registryURL)
	var token string
	if cred != nil {
		token = cred.Token
	}

	return hub.NewClient(hub.Options{
		BaseURL: registryURL,
		Token:   token,
	})
}

// loadEntriesFromPaths loads registry entries from the specified paths.
// Supports both directories (loaded via LoadFS) and .wapp files (loaded via PackReader).
func loadEntriesFromPaths(ctx context.Context, paths []string, logger *zap.Logger) ([]regapi.Entry, error) {
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		return nil, ErrTranscoderNotFound
	}

	ldr := boot.GetLoader(ctx)
	if ldr == nil {
		return nil, ErrLoaderNotFound
	}

	var entries []regapi.Entry

	for _, path := range paths {
		stat, err := os.Stat(path)
		if os.IsNotExist(err) {
			logger.Warn("path not found, skipping", zap.String("path", path))
			continue
		}

		var loadedEntries []regapi.Entry

		if stat.IsDir() {
			// Directory: load via FS
			dirFS := os.DirFS(path)
			loadedEntries, err = ldr.LoadFS(ctx, dirFS)
			if err != nil {
				return nil, NewLoadFromPathError(path, err)
			}
		} else if filepath.Ext(path) == ".wapp" {
			// Wapp file: load via PackReader
			loadedEntries, err = loadEntriesFromWapp(path, dtt)
			if err != nil {
				return nil, NewLoadFromPathError(path, err)
			}
		} else {
			logger.Warn("unknown path type, skipping", zap.String("path", path))
			continue
		}

		entries = append(entries, loadedEntries...)
	}

	pipeline := build.New(
		stages.Override(),
		stages.Disable(),
		stages.Link(),
	)

	if err := pipeline.Execute(ctx, &entries); err != nil {
		return nil, NewExecutePipelineError(err)
	}

	return entries, nil
}

// loadEntriesFromWapp loads entries from a .wapp file.
func loadEntriesFromWapp(path string, dtt payload.Transcoder) ([]regapi.Entry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader, err := NewPackReader(file, dtt)
	if err != nil {
		return nil, err
	}

	return reader.GetEntries()
}

// loadEntriesToRegistry loads entries into the registry using LoadState to restore from history.
func loadEntriesToRegistry(ctx context.Context, entries []regapi.Entry, logger *zap.Logger, _ bool) error {
	reg := regapi.GetRegistry(ctx)
	if reg == nil {
		return ErrRegistryNotFound
	}

	resolver := regapi.GetResolver(ctx)
	if resolver == nil {
		return ErrResolverNotFound
	}

	// Check for duplicate entry IDs
	entryByID := make(map[string]int)
	for _, entry := range entries {
		entryByID[entry.ID.String()]++
	}

	duplicateCount := 0
	duplicateIDs := make([]string, 0)
	for id, count := range entryByID {
		if count > 1 {
			duplicateCount += count - 1
			duplicateIDs = append(duplicateIDs, fmt.Sprintf("%s (x%d)", id, count))
		}
	}

	if duplicateCount > 0 {
		logger.Warn("duplicate entries detected (will use last definition)",
			zap.Int("duplicates", duplicateCount),
			zap.Strings("affected", duplicateIDs))
	}

	logger.Info("creating baseline state from entries", zap.Int("entry_count", len(entries)))

	baselineState := make(regapi.State, 0, len(entries))
	for _, entry := range entries {
		baselineState = append(baselineState, entry)
	}

	logger.Info("baseline state created", zap.Int("entry_count", len(baselineState)))

	hist := reg.History()
	head, err := hist.Head()
	switch {
	case err != nil:
		logger.Info("no history found, initializing registry with baseline state at v0")
		currentVer, err := reg.Current()
		if err != nil {
			return NewGetCurrentVersionError(err)
		}
		head = currentVer
	case head.ID() > 0:
		logger.Info("restoring registry state from history", zap.Uint("version", head.ID()))
	default:
		logger.Info("initializing registry with baseline state at v0")
	}

	if err := reg.LoadState(ctx, baselineState, head); err != nil {
		return err // Already wrapped by registry with proper context
	}

	logger.Debug("registry state loaded", zap.Uint("version", head.ID()))
	return nil
}

// PackReader wraps wapp.Reader for reading pack files.
type PackReader struct {
	reader *wapp.Reader
}

// NewPackReader creates a new pack reader from an io.ReaderAt.
// The transcoder parameter is kept for API compatibility but not used with wapp.
func NewPackReader(r io.ReaderAt, _ payload.Transcoder) (*PackReader, error) {
	reader, err := wapp.NewReader(r)
	if err != nil {
		return nil, err
	}
	return &PackReader{reader: reader}, nil
}

// GetEntries returns the entries from the pack file.
func (pr *PackReader) GetEntries() ([]regapi.Entry, error) {
	wappEntries, err := pr.reader.GetEntries()
	if err != nil {
		return nil, err
	}

	entries := make([]regapi.Entry, len(wappEntries))
	for i, we := range wappEntries {
		entries[i] = regapi.Entry{
			ID:   regapi.NewID(we.ID.Namespace, we.ID.Name),
			Kind: regapi.Kind(we.Kind),
			Meta: attrs.NewBagFrom(we.Meta),
			Data: payload.New(we.Data),
		}
	}
	return entries, nil
}

// ApplyToRegistry applies entries to the registry using topology-sorted change set.
func ApplyToRegistry(ctx context.Context, entries []regapi.Entry, resolver regapi.DependencyResolver, reg regapi.Registry, logger *zap.Logger) error {
	logger.Debug("building change set from entries")
	changeSet, err := regtop.CreateChangeSetFromEntries(entries, resolver)
	if err != nil {
		return NewBuildChangeSetError(err)
	}
	logger.Debug("change set built")

	logger.Info("applying change set to registry", zap.Int("entry_count", len(entries)))
	version, err := reg.Apply(ctx, changeSet)
	if err != nil {
		logger.Error("apply failed", zap.Error(err))
		return NewApplyEntriesError(err)
	}

	logger.Info("entries applied to registry", zap.Any("version", version))
	return nil
}

// ConvertToWappEntries converts registry entries to wapp entries for packing.
func ConvertToWappEntries(entries []regapi.Entry) []wapp.Entry {
	result := make([]wapp.Entry, len(entries))
	for i, e := range entries {
		var data any
		if e.Data != nil {
			data = e.Data.Data()
		}
		result[i] = wapp.Entry{
			ID:   wapp.NewID(e.ID.NS, e.ID.Name),
			Kind: string(e.Kind),
			Meta: wapp.Metadata(e.Meta),
			Data: data,
		}
	}
	return result
}
