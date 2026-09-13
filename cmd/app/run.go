// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
	"github.com/wippyai/runtime/cmd/wippy/cmd"
)

// Options selects the embedded application and its native host components.
// DataEnv maps application-owned environment variables to paths within StateDir.
// Existing environment values remain explicit user overrides.
type Options struct {
	Launch Launch
	// Baseline selects ordinary startup code: "activated" (also the default)
	// or "embedded". Both retain the selected state's registry history.
	Baseline   string
	DataEnv    map[string]string
	Components []boot.Component
	Name       string
	Command    string
	Mode       string
	Bundle     Bundle
}

var applicationName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
var environmentName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// Run is the process entry point for a standalone application. Application
// arguments follow `run`; `runtime` exposes the Wippy CLI, including
// Hub authentication, update and source inspection commands.
func Run(ctx context.Context, options Options, args []string) error {
	if ctx == nil {
		return fmt.Errorf("application context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !applicationName.MatchString(options.Name) {
		return fmt.Errorf("invalid application name")
	}
	if options.Mode != "base" && options.Mode != "bootstrap" {
		return fmt.Errorf("application mode must be base or bootstrap")
	}
	if options.Command == "" {
		return fmt.Errorf("application command is required")
	}
	if options.Baseline != "" && options.Baseline != "activated" && options.Baseline != "embedded" {
		return fmt.Errorf("application baseline must be activated or embedded")
	}
	flags := flag.NewFlagSet(options.Name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	state := flags.String("state-dir", "", "application state directory")
	base := flags.Bool("base", false, "run the embedded recovery baseline")
	command := flags.String("command", options.Command, "application command")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *state == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		*state = filepath.Join(config, options.Name)
	}
	stateDir, err := filepath.Abs(*state)
	if err != nil {
		return err
	}
	remaining := append([]string(nil), flags.Args()...)
	ordinary := !*base && (len(remaining) == 0 || (remaining[0] != "runtime" && remaining[0] != "update"))
	if options.Launch != nil && ordinary {
		if len(remaining) > 0 && remaining[0] == "run" {
			remaining = remaining[1:]
		}
		directory, err := os.Getwd()
		if err != nil {
			return err
		}
		request := LaunchRequest{Name: options.Name, Module: options.Bundle.Root, StateDir: stateDir,
			Directory: directory, Command: *command, Arguments: append([]string(nil), remaining...)}
		return launch(ctx, options.Launch, request, func(ctx context.Context, owner OwnerOptions) error {
			selected := *command
			if owner.Command != "" {
				selected = owner.Command
			}
			arguments := remaining
			if owner.Arguments != nil {
				arguments = owner.Arguments
			}
			return runApplication(ctx, options, stateDir, false, selected, append([]string{"run"}, arguments...), owner)
		})
	}
	return runApplication(ctx, options, stateDir, *base, *command, remaining, OwnerOptions{})
}

func runApplication(ctx context.Context, options Options, stateDir string, base bool, command string, remaining []string, owner OwnerOptions) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	unlock, err := lockApplication(stateDir)
	if err != nil {
		if errors.Is(err, errLockBusy) {
			return fmt.Errorf("%w: %w", ErrBusy, err)
		}
		return err
	}
	defer unlock()
	var overrides boot.Config
	if owner.Prepare != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		resources, err := owner.Prepare(ctx)
		if resources.Close != nil {
			defer func() { result = errors.Join(result, resources.Close()) }()
		}
		if err != nil {
			return err
		}
		if !resources.Deadline.IsZero() {
			var cancel context.CancelFunc
			ctx, cancel = context.WithDeadline(ctx, resources.Deadline)
			defer cancel()
		}
		overrides = resources.Config
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := configureDataEnvironment(stateDir, options.DataEnv); err != nil {
		return err
	}
	ordinary := len(remaining) == 0 || (remaining[0] != "runtime" && remaining[0] != "update")
	deployment, historyPath, err := selectLaunchDeployment(stateDir, options.Bundle, ordinary && options.Baseline == "embedded", base, options.Mode)
	if err != nil {
		return err
	}
	lockPath, err := options.Bundle.Seed(deployment)
	if err != nil {
		return err
	}
	if err := seedDependencyCache(stateDir, deployment, options.Bundle); err != nil {
		return err
	}
	if len(remaining) > 0 && remaining[0] == "update" {
		if base {
			return fmt.Errorf("base recovery cannot be updated")
		}
		return updateDeployment(ctx, options, stateDir, deployment, remaining[1:], runChild)
	}
	runtimeArgs := []string{"run", "--silent", "--", command}
	if len(remaining) > 0 && remaining[0] == "runtime" {
		if base {
			return fmt.Errorf("base recovery only runs the embedded application")
		}
		runtimeArgs = remaining[1:]
	} else {
		if len(remaining) > 0 && remaining[0] == "run" {
			remaining = remaining[1:]
		}
		runtimeArgs = append(runtimeArgs, remaining...)
	}
	configFiles := []string{}
	configPath := filepath.Join(stateDir, ".wippy.yaml")
	if _, err := os.Stat(configPath); err == nil {
		configFiles = append(configFiles, configPath)
	} else if !os.IsNotExist(err) {
		return err
	}
	overrides = launchOverrides(overrides, historyPath)
	// Keep dependency artifacts in application state so a new executable can
	// restore an older registry graph without contacting the Hub. The lock and
	// deployment source files remain selected independently above.
	overrides = bootconfig.Merge(overrides, boot.NewConfig(boot.WithSection("registry", map[string]any{
		"dependency_vendor_dir": dependencyVendorDirectory(stateDir),
	})))
	return cmd.ExecuteWithOptions(ctx, cmd.ExecuteOptions{
		Args:        runtimeArgs,
		LockFile:    lockPath,
		ConfigFiles: configFiles,
		Components:  options.Components,
		Overrides:   overrides,
	})
}

func configureDataEnvironment(state string, variables map[string]string) error {
	names := make([]string, 0, len(variables))
	for name := range variables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		relative := variables[name]
		if !environmentName.MatchString(name) || !filepath.IsLocal(relative) || strings.ContainsRune(relative, 0) {
			return fmt.Errorf("invalid application data environment binding %q", name)
		}
	}
	for _, name := range names {
		if _, exists := os.LookupEnv(name); !exists {
			if err := os.Setenv(name, filepath.Join(state, variables[name])); err != nil {
				return err
			}
		}
	}
	return nil
}
