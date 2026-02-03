package cmd

import (
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

var installCmd = &cobra.Command{
	Use:   "install [module...]",
	Short: "Install dependencies from lock file",
	Long: `Install dependencies from wippy.lock file

Downloads and installs all modules specified in the lock file.
If the lock file is missing, runs 'wippy init' followed by 'wippy update'.

Modules are installed to the vendor directory specified in the lock file.

When module names are provided as arguments, only those modules are processed.
Use with --force or --repair to target specific modules:
  wippy install --repair keeper/keeper wippy/relay`,
	RunE: runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)

	installCmd.Flags().StringP("lock-file", "l", defaultLockFile, "path to lock file")
	installCmd.Flags().Bool("force", false, "bypass cache and always download modules")
	installCmd.Flags().Bool("repair", false, "verify entry hashes and re-download if mismatch")
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
		return NewLoadLockFileError(err)
	}

	if err := lock.Validate(lockObj); err != nil {
		return NewInvalidLockFileError(err)
	}

	modules := lockObj.GetModules()
	if len(modules) == 0 {
		logger.Info("no modules to install")
		return nil
	}

	// Filter modules if specific modules are requested
	if len(args) > 0 {
		targetModules := make(map[string]bool)
		for _, arg := range args {
			targetModules[arg] = true
		}

		filtered := make([]lock.Module, 0)
		for _, mod := range modules {
			if targetModules[mod.Name] {
				filtered = append(filtered, mod)
			}
		}

		if len(filtered) == 0 {
			logger.Warn("no matching modules found in lock file", zap.Strings("requested", args))
			return nil
		}

		modules = filtered
		logger.Info("installing specific modules", zap.Int("count", len(modules)))
	} else {
		logger.Info("modules to install", zap.Int("count", len(modules)))
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
		return NewCreateHubClientError(err)
	}

	lockDir := filepath.Dir(lockObj.Path())
	vendorPath := lockObj.GetVendorPath()
	vendorDir := filepath.Join(lockDir, vendorPath)
	shouldUnpack := lockObj.ShouldUnpackModules()

	force, _ := cmd.Flags().GetBool("force")

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

		if !force {
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

		if downloadInfo.URL == "" {
			return NewNoContentDownloadedError(moduleRef)
		}

		// Download .wapp file
		wappPath := filepath.Join(vendorDir, lock.WappPath(modName, module.Version))
		if err := hubClient.DownloadToFile(app.Ctx, downloadInfo.URL, wappPath); err != nil {
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
			logger.Warn("failed to update lock file", zap.Error(err))
		}
	}

	logMsg := "installation complete"
	logFields := []zap.Field{
		zap.Int("installed", installed),
		zap.Int("cached", cached),
		zap.Int("total", len(modules)),
	}
	logger.Info(logMsg, logFields...)

	return nil
}
