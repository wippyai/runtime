// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"

	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
)

// ExecuteOptions configures a native application's use of the Wippy CLI.
// Components are additional host-selected components, never package grants.
// ConfigFiles are explicit required files; an empty list disables ambient config.
// Defaults have lower precedence than packed application and file configuration.
type ExecuteOptions struct {
	Defaults boot.Config
	// Overrides are host-selected settings applied after package and CLI settings.
	Overrides   boot.Config
	LockFile    string
	Args        []string
	Components  []boot.Component
	ConfigFiles []string
}

var nativeExecution atomic.Bool
var nativeOptions *ExecuteOptions

// ExecuteWithOptions runs one command using the canonical Wippy command paths.
// Like Execute, it owns the process command state and signal handlers. Call it
// once from main, never concurrently or from an application actor.
func ExecuteWithOptions(ctx context.Context, options ExecuteOptions) error {
	if ctx == nil {
		return fmt.Errorf("execution context is required")
	}
	if options.LockFile == "" {
		return fmt.Errorf("deployment lock file is required")
	}
	absolute, err := filepath.Abs(options.LockFile)
	if err != nil {
		return err
	}
	if err := validateNativeComponents(options.Components); err != nil {
		return err
	}
	if !nativeExecution.CompareAndSwap(false, true) {
		return fmt.Errorf("native command execution may only be called once")
	}
	options.LockFile = absolute
	options.Components = append([]boot.Component(nil), options.Components...)
	options.ConfigFiles = append([]string(nil), options.ConfigFiles...)
	nativeOptions = &options
	defaultLockFile = absolute
	configFiles = options.ConfigFiles
	// Commands declare their flag defaults at package initialization time.
	// Keep every lock-aware command on the same host-selected deployment.
	var configure func(*cobra.Command) error
	configure = func(command *cobra.Command) error {
		if flag := command.Flags().Lookup("lock-file"); flag != nil {
			if err := flag.Value.Set(absolute); err != nil {
				return err
			}
			flag.DefValue = absolute
		}
		for _, child := range command.Commands() {
			if err := configure(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := configure(rootCmd); err != nil {
		return err
	}
	rootCmd.SetArgs(options.Args)
	return rootCmd.ExecuteContext(ctx)
}

func validateNativeComponents(additional []boot.Component) error {
	names := make(map[boot.Name]bool)
	for _, component := range StandardComponents() {
		names[component.Name()] = true
	}
	for _, component := range additional {
		if component == nil || component.Name() == "" {
			return fmt.Errorf("native component must have a name")
		}
		if names[component.Name()] {
			return fmt.Errorf("duplicate native component %q", component.Name())
		}
		names[component.Name()] = true
	}
	return nil
}

func selectedComponents() []boot.Component {
	components := StandardComponents()
	if nativeOptions != nil {
		components = append(components, nativeOptions.Components...)
	}
	return components
}

func nativeBootDefaults() boot.Config {
	if nativeOptions == nil {
		return nil
	}
	return nativeOptions.Defaults
}

func applyNativeDeploymentConfig(cfg boot.Config) boot.Config {
	if nativeOptions == nil {
		return cfg
	}
	cfg = bootconfig.Merge(cfg, nativeOptions.Overrides)
	return bootconfig.Merge(cfg, boot.NewConfig(boot.WithSection("registry", map[string]any{
		"dependency_lock_path": nativeOptions.LockFile,
	})))
}
