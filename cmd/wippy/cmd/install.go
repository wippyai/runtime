package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ponyruntime/pony/deps/graph"
	"github.com/ponyruntime/pony/deps/lock"
	"github.com/ponyruntime/pony/deps/storage"
	"github.com/spf13/cobra"
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

	installCmd.Flags().StringP("lock-file", "l", "wippy.lock", "path to lock file")
	installCmd.Flags().Bool("force", false, "bypass cache and always download modules")
	installCmd.Flags().Bool("repair", false, "verify entry hashes and re-download if mismatch")
}

func runInstall(cmd *cobra.Command, args []string) error {
	app, err := InitApp(cmd.Context())
	if err != nil {
		return fmt.Errorf("init app: %w", err)
	}

	logger := app.Logger.Named("install")

	lockFileName, _ := cmd.Flags().GetString("lock-file")

	lockPath := lockFileName
	if !filepath.IsAbs(lockPath) {
		lockPath = filepath.Join(".", lockPath)
	}

	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		logger.Info("lock file not found, running init and update")

		if err := runInit(cmd, args); err != nil {
			return fmt.Errorf("init failed: %w", err)
		}

		return runUpdate(cmd, args)
	}

	logger.Info("installing dependencies", zap.String("lock_file", lockPath))

	lockObj, err := lock.New(lockPath)
	if err != nil {
		return fmt.Errorf("load lock file: %w", err)
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

	lockDir := filepath.Dir(lockPath)
	vendorPath := lockObj.GetVendorPath()
	vendorDir := filepath.Join(lockDir, vendorPath)
	storageImpl := storage.NewFileSystemStorage(vendorDir)

	force, _ := cmd.Flags().GetBool("force")
	repair, _ := cmd.Flags().GetBool("repair")

	installed := 0
	cached := 0
	repaired := 0

	for _, module := range modules {
		name, err := graph.ParseName(module.Name)
		if err != nil {
			return fmt.Errorf("parse module name %s: %w", module.Name, err)
		}

		modulePath := lock.ModulePath(name, module.Version)

		var exists bool
		if !force {
			exists, err = storageImpl.Exists(modulePath)
			if err != nil {
				return fmt.Errorf("check module %s: %w", module.Name, err)
			}
		}

		// Verify hash if repair flag is set
		if exists && repair {
			if module.LocalHash != "" {
				logger.Debug("verifying module hash",
					zap.String("module", module.Name))

				computedHash, err := storageImpl.ComputeHash(modulePath, app.Ctx, app.Transcoder, app.Loader)
				if err != nil {
					logger.Warn("failed to compute hash, will re-download",
						zap.String("module", module.Name),
						zap.Error(err))
					exists = false
				} else if computedHash != module.LocalHash {
					logger.Info("hash mismatch, re-downloading",
						zap.String("module", module.Name),
						zap.String("expected", module.LocalHash),
						zap.String("actual", computedHash))
					exists = false
					repaired++
				}
			}
		}

		if exists {
			logger.Info("module already installed, skipping download",
				zap.String("module", module.Name),
				zap.String("version", module.Version))
			cached++
			continue
		}

		logger.Info("downloading module",
			zap.String("module", module.Name),
			zap.String("version", module.Version))

		if module.Hash == "" {
			return fmt.Errorf("module %s has no hash in lock file", module.Name)
		}

		results, err := app.RegistryClient.Download(app.Ctx, []string{module.Hash})
		if err != nil {
			return fmt.Errorf("download module %s: %w", module.Name, err)
		}

		if len(results) == 0 {
			return fmt.Errorf("no content downloaded for module %s", module.Name)
		}

		if err := storageImpl.StoreProtoFiles(modulePath, results[0].Files); err != nil {
			return fmt.Errorf("store module %s: %w", module.Name, err)
		}

		// Compute local hash from loaded entries
		computedHash, err := storageImpl.ComputeHash(modulePath, app.Ctx, app.Transcoder, app.Loader)
		if err != nil {
			logger.Warn("failed to compute module hash",
				zap.String("module", module.Name),
				zap.Error(err))
		} else {
			// Update module with computed hash
			module.LocalHash = computedHash
			lockObj.SetModule(module)
			logger.Debug("computed local hash",
				zap.String("module", module.Name),
				zap.String("hash", computedHash))
		}

		logger.Info("installed module",
			zap.String("module", module.Name),
			zap.String("version", module.Version))
		installed++
	}

	// Save updated lock file with local hashes
	if installed > 0 {
		if err := lockObj.Write(); err != nil {
			logger.Warn("failed to update lock file with hashes", zap.Error(err))
		}
	}

	logMsg := "installation complete"
	logFields := []zap.Field{
		zap.Int("installed", installed),
		zap.Int("cached", cached),
		zap.Int("total", len(modules)),
	}
	if repaired > 0 {
		logFields = append(logFields, zap.Int("repaired", repaired))
	}
	logger.Info(logMsg, logFields...)

	return nil
}
