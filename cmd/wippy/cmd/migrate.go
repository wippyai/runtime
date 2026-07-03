// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/function"
	logapi "github.com/wippyai/runtime/api/logs"
	moduleapi "github.com/wippyai/runtime/api/modules"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/deps/client"
	appinit "github.com/wippyai/runtime/cmd/internal/app"
	"github.com/wippyai/runtime/cmd/internal/banner"
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
	"github.com/wippyai/runtime/cmd/internal/entries"
	"github.com/wippyai/runtime/cmd/internal/shutdown"
	embedpkg "github.com/wippyai/runtime/service/fs/embed"
	"go.uber.org/zap"
)

// Migration entrypoints in the migration module. Both are callable functions
// that discover every target database in the registry and drive the migration
// runner. migration_bootloader also runs on a normal boot; the migrate command
// invokes these on their own and exits.
var (
	migrationBootloaderID = registry.NewID("wippy.migration", "migration_bootloader")
	migrationRollbackID   = registry.NewID("wippy.migration", "migration_rollback")
)

// bootloaderServiceEntry is the auto-start orchestrator that runs every
// bootloader (including migrations) on a normal boot. The migrate command
// disables it so that the only thing touching the database is the explicit
// migration call below, not a second run racing it.
const bootloaderServiceEntry = "wippy.bootloader:bootloader.service"

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Manage database migrations",
	Long:  `Run database migrations without starting the full application.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	},
}

var migrateUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Apply all pending migrations",
	Long: `Apply all pending migrations to every target database, then exit.

Boots a runtime from wippy.lock the same way 'wippy run' does, applies pending
migrations, prints a summary, and exits. The exit code is 0 when migrations were
applied or there was nothing to do, and non-zero when a migration failed.

Examples:
  wippy migrate up
  wippy migrate up -o "app:db:options.sslmode=disable"`,
	Args: cobra.NoArgs,
	RunE: runMigrateUp,
}

var migrateDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Roll back the last N applied migrations",
	Long: `Roll back the most recently applied migrations, then exit.

Rolls back --count migrations (default 1) from every target database, newest
first, then exits. The exit code is 0 on success and non-zero when a rollback
failed.

Examples:
  wippy migrate down
  wippy migrate down --count 3
  wippy migrate down -n 3 -o "app:db:options.sslmode=disable"`,
	Args: cobra.NoArgs,
	RunE: runMigrateDown,
}

func init() {
	rootCmd.AddCommand(migrateCmd)
	migrateCmd.AddCommand(migrateUpCmd)
	migrateCmd.AddCommand(migrateDownCmd)

	// Mirror the config flags 'run' accepts so callers can point the command at
	// a database (e.g. -o "app:db:options.sslmode=disable" in CI).
	for _, c := range []*cobra.Command{migrateUpCmd, migrateDownCmd} {
		c.Flags().StringSliceP("override", "o", nil, "Override entry values (format: namespace:entry:field=value)")
		c.Flags().StringArray("set", nil, "Override a .wippy.yaml config value (format: section.path=value, repeatable)")
		c.Flags().StringArray("profile", nil, "Apply a runtime profile from .wippy.yaml (repeatable, applied in order)")
	}

	migrateDownCmd.Flags().IntP("count", "n", 1, "Number of migrations to roll back")
}

func runMigrateUp(cmd *cobra.Command, _ []string) error {
	return runMigrate(cmd, func(ctx context.Context, logger *zap.Logger) error {
		return callMigration(ctx, logger, migrationBootloaderID, nil, "up",
			"Migrations applied.", "No migrations to apply.")
	})
}

func runMigrateDown(cmd *cobra.Command, _ []string) error {
	count, _ := cmd.Flags().GetInt("count")
	if count < 1 {
		count = 1
	}
	return runMigrate(cmd, func(ctx context.Context, logger *zap.Logger) error {
		opts := map[string]any{"count": count}
		return callMigration(ctx, logger, migrationRollbackID, opts, "down",
			"Migrations rolled back.", "No migrations to roll back.")
	})
}

// runMigrate boots a headless runtime, runs the given migration action, then
// shuts down and exits with a status code that reflects whether the action
// succeeded.
func runMigrate(cmd *cobra.Command, action func(context.Context, *zap.Logger) error) error {
	initMemoryLimit()
	banner.Print(silentLogs)

	logger, err := createCommandLogger()
	if err != nil {
		return NewCreateLoggerError(err)
	}
	defer func() { _ = logger.Sync() }()

	cfg, err := loadRuntimeConfig(cmd, logger)
	if err != nil {
		logger.Error("failed to resolve runtime config", zap.Error(err))
		return err
	}

	// Suppress the auto-start bootloader orchestrator so it does not run
	// migrations in parallel with our explicit call (which would race, and for
	// 'down' would re-apply what we are rolling back).
	cfg = bootconfig.Merge(cfg, boot.NewConfig(boot.WithSection("disable", map[string]any{
		"entries": []string{bootloaderServiceEntry},
	})))

	ctx, err := bootpkg.NewBootstrapContext(logger, cfg)
	if err != nil {
		logger.Error("failed to initialize bootstrap context", zap.Error(err))
		return NewInitializeBootstrapContextError(err)
	}
	ctx = moduleapi.WithSourceRootRegistry(ctx)

	registryClient := client.NewRegistryClientFromConfig(boot.GetConfig(ctx))
	ctx = appinit.WithRegistryClient(ctx, registryClient)

	logger = logapi.GetLogger(ctx).Named("migrate")

	embedReg := embedpkg.NewRegistry()
	ctx = embedapi.WithRegistry(ctx, embedReg)
	defer embedReg.Close()

	components := StandardComponents()
	ctx, extensionComponents, err := loadExtensionComponents(ctx, logger, components)
	if err != nil {
		logger.Error("failed to load extensions", zap.Error(err))
		return err
	}
	components = append(components, extensionComponents...)

	loader, err := bootpkg.NewLoader(components...)
	if err != nil {
		logger.Error("failed to create loader", zap.Error(err))
		return NewCreateLoaderError(err)
	}

	ctx, err = loader.Load(ctx)
	if err != nil {
		logger.Error("load failed", zap.Error(err))
		return NewLoadComponentsError(err)
	}

	if err := loader.Start(ctx); err != nil {
		logger.Error("start failed", zap.Error(err))
		return NewStartComponentsError(err)
	}

	if err := entries.LoadFromLockFile(ctx, logger); err != nil {
		logger.Error("entry loading failed", zap.Error(err))
		return err
	}

	actionErr := action(ctx, logger)

	// Always shut down cleanly so DB connections and services release before we
	// exit, regardless of whether the migration action succeeded.
	exitCode := shutdown.Perform(ctx, loader, logger, silentLogs)

	if actionErr != nil {
		_ = logger.Sync()
		os.Exit(1)
	}
	if exitCode != 0 {
		_ = logger.Sync()
		os.Exit(exitCode)
	}
	return nil
}

// callMigration invokes a migration function synchronously and interprets its
// result. The migration functions report failures in their return value
// (status == "error") rather than as a Go error, so we decode the returned
// table to learn whether a migration failed.
func callMigration(
	ctx context.Context,
	logger *zap.Logger,
	id registry.ID,
	options map[string]any,
	label, okMsg, noopMsg string,
) error {
	funcReg := function.GetRegistry(ctx)
	if funcReg == nil {
		return NewFunctionRegistryNotAvailableError()
	}

	logger.Info("running migration action", zap.String("action", label), zap.String("function", id.String()))

	if options == nil {
		options = map[string]any{}
	}
	task := runtimeapi.Task{
		ID:       id,
		Payloads: payload.Payloads{payload.New(options)},
	}

	result, err := funcReg.Call(ctx, task)
	if err != nil {
		logger.Error("migration call failed", zap.Error(err))
		return NewMigrationCallError(err)
	}
	if result != nil && result.Error != nil {
		logger.Error("migration returned an error", zap.Error(result.Error))
		return NewMigrationCallError(result.Error)
	}

	status, message := migrationResult(result)

	switch status {
	case "error":
		logger.Error("migration failed", zap.String("message", message))
		fmt.Fprintln(os.Stderr, "migration failed: "+message)
		return NewMigrationFailedError(message)
	case "skipped":
		logger.Info("nothing to do", zap.String("message", message))
		fmt.Println(fallback(message, noopMsg))
	default:
		// "success" or an unrecognized-but-non-error status.
		logger.Info("migration action complete", zap.String("message", message))
		fmt.Println(fallback(message, okMsg))
	}
	return nil
}

// migrationResult pulls the status and message out of a migration function's
// returned table.
func migrationResult(result *runtimeapi.Result) (status, message string) {
	if result == nil || result.Value == nil {
		return "", ""
	}
	data, ok := result.Value.Data().(map[string]any)
	if !ok {
		return "", ""
	}
	status, _ = data["status"].(string)
	message, _ = data["message"].(string)
	return status, message
}

func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
