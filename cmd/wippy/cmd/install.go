package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
	appinit "github.com/wippyai/runtime/cmd/internal/app"
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

	lockDir := filepath.Dir(lockPath)
	vendorPath := lockObj.GetVendorPath()
	vendorDir := filepath.Join(lockDir, vendorPath)

	force, _ := cmd.Flags().GetBool("force")

	installed := 0
	cached := 0

	for _, module := range modules {
		parts := strings.SplitN(module.Name, "/", 2)
		if len(parts) != 2 {
			return NewParseModuleNameError(module.Name, fmt.Errorf("invalid format, expected org/module"))
		}
		org, name := parts[0], parts[1]

		// Store as .wapp file
		wappPath := filepath.Join(vendorDir, org, fmt.Sprintf("%s-%s.wapp", name, module.Version))

		var exists bool
		if !force {
			if _, err := os.Stat(wappPath); err == nil {
				exists = true
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

		// Get download URL from hub
		downloadInfo, err := hubClient.GetDownloadURL(app.Ctx, &hub.DownloadParams{
			Org:     org,
			Module:  name,
			Version: module.Version,
		})
		if err != nil {
			return NewDownloadModuleError(module.Name, err)
		}

		if downloadInfo.URL == "" {
			return NewNoContentDownloadedError(module.Name)
		}

		// Download .wapp file
		if err := hubClient.DownloadToFile(app.Ctx, downloadInfo.URL, wappPath); err != nil {
			return NewDownloadModuleError(module.Name, err)
		}

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
