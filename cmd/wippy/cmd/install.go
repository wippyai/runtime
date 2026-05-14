// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
	appinit "github.com/wippyai/runtime/cmd/internal/app"
	"github.com/wippyai/runtime/cmd/internal/entries"
	"go.uber.org/zap"
)

// downloadWappViaHubOrLegacy attempts the hub-mediated download first
// (single HTTPS hop to the hub, hub retries into S3); if the hub doesn't
// advertise the endpoint yet (404 / 405) and a presigned S3 URL is
// available, falls back to the legacy direct-to-S3 flow.
func downloadWappViaHubOrLegacy(ctx context.Context, hubClient *hub.Client, info *hub.DownloadInfo, wappPath string) error {
	if info.Digest != "" {
		err := hubClient.DownloadViaHub(ctx, info.Digest, wappPath)
		if err == nil {
			return nil
		}
		if !hub.IsHubEndpointMissing(err) || info.URL == "" {
			return err
		}
	}
	return hubClient.DownloadToFile(ctx, info.URL, wappPath)
}

var installCmd = &cobra.Command{
	Use:   "install [module...]",
	Short: "Install dependencies from lock file",
	Long: `Install dependencies from wippy.lock file

Downloads and installs all modules specified in the lock file.
If the lock file is missing, runs 'wippy init' followed by 'wippy update'.

Modules are installed to the vendor directory specified in the lock file.
Local replacements are validated by the lock file and skipped by install because
the runtime loads those modules directly from their replacement paths.

When module names are provided as arguments, only those modules are processed.
Use with --refresh to re-download modules when cache might be stale:
  wippy install --refresh
  wippy install --refresh acme/ui wippy/relay`,
	RunE: runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)

	installCmd.Flags().StringP("lock-file", "l", defaultLockFile, "path to lock file")
	installCmd.Flags().Bool("refresh", false, "re-download modules even if already cached")
	installCmd.Flags().Bool("force", false, "alias for --refresh")
	installCmd.Flags().Bool("repair", false, "alias for --refresh")
	installCmd.Flags().String("registry", "", "registry URL (default: from credentials)")
}

func runInstall(cmd *cobra.Command, args []string) error {
	app, err := appinit.Init(cmd.Context(), verbose, veryVerbose, console, silentLogs, appStartTime)
	if err != nil {
		return NewInitAppError(err)
	}

	logger := app.Logger.Named("install")

	lockPath, _ := cmd.Flags().GetString("lock-file")
	registryURL, _ := cmd.Flags().GetString("registry")

	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		logger.Info("lock file not found, running init and update")

		if err := runInit(cmd, args); err != nil {
			return NewInitFailedError(err)
		}

		return runUpdate(cmd, args)
	}

	logger.Info("installing dependencies", zap.String("lock_file", lockPath))

	lockObj, err := lock.New(lockPath)
	if err != nil {
		return NewLoadLockFileError(fmt.Errorf("lock file %s: %w", lockPath, err))
	}

	if err := lock.Validate(lockObj); err != nil {
		return NewInvalidLockFileError(fmt.Errorf("lock file %s: %w", lockObj.Path(), err))
	}

	selection := selectInstallModules(lockObj, args, logger)
	modules := selection.modules
	if len(args) > 0 && selection.matched == 0 {
		logger.Warn("no matching modules found in lock file", zap.Strings("requested", args))
		return nil
	}
	if len(modules) == 0 {
		if selection.skippedReplaced > 0 {
			logger.Info("all selected modules are local replacements; nothing to install",
				zap.Int("skipped_replaced", selection.skippedReplaced))
		} else {
			logger.Info("no remote modules to install")
		}
		return nil
	}
	if len(args) > 0 {
		logger.Info("installing specific remote modules",
			zap.Int("count", len(modules)),
			zap.Int("skipped_replaced", selection.skippedReplaced))
	} else {
		logger.Info("remote modules to install",
			zap.Int("count", len(modules)),
			zap.Int("skipped_replaced", selection.skippedReplaced))
	}

	// Get auth credentials
	projectDir, _ := os.Getwd()
	authCfg := bootauth.NewConfig(projectDir)
	store := bootauth.NewStore(authCfg)

	if registryURL == "" {
		registryURL = store.DefaultRegistry()
	}

	cred, _ := store.Get(registryURL)
	var token string
	if cred != nil {
		token = cred.Token
	}

	// Create hub client
	hubClient, err := hub.NewClient(hub.Options{
		BaseURL: registryURL,
		Token:   token,
	})
	if err != nil {
		return NewCreateHubClientError(fmt.Errorf("registry %s: %w", registryURL, err))
	}

	lockDir := filepath.Dir(lockObj.Path())
	vendorPath := lockObj.GetVendorPath()
	vendorDir := lock.ResolveLockPath(lockDir, vendorPath)
	shouldUnpack := lockObj.ShouldUnpackModules()

	refresh := shouldBypassInstallCache(cmd)
	if refresh {
		logger.Info("refresh enabled, bypassing module cache")
	}

	installed := 0
	cached := 0

	for _, module := range modules {
		modName, err := graph.ParseName(module.Name)
		if err != nil {
			return NewParseModuleNameError(module.Name, fmt.Errorf("invalid format, expected org/module"))
		}

		moduleRef := module.Name
		if module.Version != "" {
			moduleRef = moduleRef + "@" + module.Version
		}

		dirPath := filepath.Join(vendorDir, lock.ModulePath(modName))

		if !refresh {
			resolved := lock.ResolveModuleDir(vendorDir, modName, module.Version)
			if resolved.IsWapp {
				if shouldUnpack {
					// Unpack .wapp to directory when unpack is enabled
					logger.Info("unpacking .wapp to directory", zap.String("module", module.Name))
					if err := entries.ExtractWappToDir(resolved.Path, dirPath, lockDir); err != nil {
						return NewExtractModuleError(module.Name, err)
					}
				}
				// When unpack=false, keep .wapp as-is (already installed)
				cached++
				continue
			}
			if _, err := os.Stat(resolved.Path); err == nil {
				logger.Info("module already installed, skipping download",
					zap.String("module", module.Name),
					zap.String("version", module.Version))
				cached++
				continue
			}
		}

		logger.Info("downloading module",
			zap.String("module", module.Name),
			zap.String("version", module.Version))

		// Get download URL from hub
		downloadInfo, err := hubClient.GetDownloadURL(app.Ctx, &hub.DownloadParams{
			Org:     modName.Organization,
			Module:  modName.Module,
			Version: module.Version,
		})
		if err != nil {
			return NewDownloadModuleError(moduleRef, err)
		}

		if downloadInfo.URL == "" && downloadInfo.Digest == "" {
			return NewNoContentDownloadedError(moduleRef)
		}

		// Download .wapp file. Prefer the hub-mediated download path
		// (one HTTPS hop to the hub, hub-side retry into S3) — same
		// motivation as publish: a direct-to-S3 GET from a client on a
		// flaky network fails without retry. Fall back to the legacy
		// presigned-URL flow only if the new endpoint is missing on
		// this hub deployment.
		wappPath := filepath.Join(vendorDir, lock.WappPath(modName, module.Version))
		if err := downloadWappViaHubOrLegacy(app.Ctx, hubClient, downloadInfo, wappPath); err != nil {
			return NewDownloadModuleError(moduleRef, err)
		}

		if shouldUnpack {
			// Remove old directory (handles version updates) and extract
			if err := os.RemoveAll(dirPath); err != nil {
				return NewStoreModuleError(moduleRef, err)
			}
			if err := entries.ExtractWappToDir(wappPath, dirPath, lockDir); err != nil {
				return NewExtractModuleError(moduleRef, err)
			}
		}
		// When unpack=false, keep .wapp file as-is

		// Update hash from download info if available
		if downloadInfo.Digest != "" && module.Hash != downloadInfo.Digest {
			module.Hash = downloadInfo.Digest
			lockObj.SetModule(module)
		}

		logger.Info("installed module",
			zap.String("module", module.Name),
			zap.String("version", module.Version))
		installed++
	}

	// Save updated lock file
	if installed > 0 {
		if err := lockObj.Write(); err != nil {
			logger.Warn("failed to update lock file", zap.Error(fmt.Errorf("lock file %s: %w", lockObj.Path(), err)))
		}
	}

	logMsg := "installation complete"
	logFields := []zap.Field{
		zap.Int("installed", installed),
		zap.Int("cached", cached),
		zap.Int("skipped_replaced", selection.skippedReplaced),
		zap.Int("total", len(modules)),
	}
	logger.Info(logMsg, logFields...)
	if !refresh && installed == 0 && cached > 0 {
		logger.Info("all modules were loaded from cache; use --refresh to re-download")
	}

	return nil
}

type installSelection struct {
	modules         []lock.Module
	matched         int
	skippedReplaced int
}

func selectInstallModules(lockObj *lock.Lock, requested []string, logger *zap.Logger) installSelection {
	if logger == nil {
		logger = zap.NewNop()
	}
	if lockObj == nil {
		return installSelection{}
	}

	var requestedSet map[string]bool
	if len(requested) > 0 {
		requestedSet = make(map[string]bool, len(requested))
		for _, name := range requested {
			requestedSet[name] = true
		}
	}

	selection := installSelection{}
	for _, module := range lockObj.GetModules() {
		if requestedSet != nil && !requestedSet[module.Name] {
			continue
		}
		selection.matched++

		if repl, ok := lockObj.GetReplacement(module.Name); ok {
			logger.Info("module is replaced by local source; skipping install",
				zap.String("module", module.Name),
				zap.String("replacement", repl.To))
			selection.skippedReplaced++
			continue
		}

		selection.modules = append(selection.modules, module)
	}

	return selection
}

func shouldBypassInstallCache(cmd *cobra.Command) bool {
	return getBoolFlag(cmd, "refresh") || getBoolFlag(cmd, "force") || getBoolFlag(cmd, "repair")
}

func getBoolFlag(cmd *cobra.Command, name string) bool {
	if cmd == nil {
		return false
	}
	flag := cmd.Flags().Lookup(name)
	if flag == nil {
		return false
	}
	value, err := cmd.Flags().GetBool(name)
	return err == nil && value
}
