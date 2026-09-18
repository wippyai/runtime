// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"errors"
	"flag"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/cmd/wippy/cmd"
)

// Options selects the embedded application and its native host components.
// DataEnv maps application-owned environment variables to paths within StateDir.
// Existing environment values remain explicit user overrides.
type Options struct {
	Launch Launch
	// Baseline selects ordinary startup code: "activated" (also the default)
	// or "embedded". Both retain the selected state's registry history.
	Baseline string
	// DefaultStateDir lets the executable select its application state when the
	// invocation does not provide --state-dir. Explicit state always wins.
	DefaultStateDir func() (string, error)
	DataEnv         map[string]string
	Components      []boot.Component
	Name            string
	Command         string
	Mode            string
	Bundle          Bundle
}

// Reserved commands address the runner and the Wippy CLI rather than the
// embedded application.
const (
	reservedUpdate  = "update"
	reservedRuntime = "runtime"
)

// Baseline values name the startup code an executable selects.
const (
	baselineActivated = "activated"
	baselineEmbedded  = "embedded"
)

var applicationName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
var environmentName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// reservedCommand names the runner command the invocation selects, or an empty
// string when the arguments address the embedded application.
func reservedCommand(args []string) string {
	if len(args) == 0 {
		return ""
	}
	switch args[0] {
	case reservedUpdate, reservedRuntime:
		return args[0]
	}
	return ""
}

// Run is the process entry point for a standalone application. Application
// arguments follow `run`; `runtime` exposes the Wippy CLI, including
// Hub authentication, update and source inspection commands.
func Run(ctx context.Context, options Options, args []string) error {
	if !applicationName.MatchString(options.Name) {
		return NewInvalidApplicationNameError(options.Name)
	}
	if options.Mode != "base" && options.Mode != "bootstrap" {
		return NewInvalidApplicationModeError(options.Mode)
	}
	if options.Command == "" {
		return NewMissingApplicationCommandError()
	}
	if options.Baseline != "" && options.Baseline != baselineActivated && options.Baseline != baselineEmbedded {
		return NewInvalidBaselineError(options.Baseline)
	}
	if err := validateDataEnvironment(options.DataEnv); err != nil {
		return err
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
		if options.DefaultStateDir != nil {
			selected, err := options.DefaultStateDir()
			if err != nil {
				return NewDefaultStateResolveError(err)
			}
			if selected == "" {
				return ErrEmptyDefaultState
			}
			*state = selected
		} else {
			config, err := os.UserConfigDir()
			if err != nil {
				return err
			}
			*state = filepath.Join(config, options.Name)
		}
	}
	stateDir, err := filepath.Abs(*state)
	if err != nil {
		return err
	}
	remaining := append([]string(nil), flags.Args()...)
	ordinary := !*base && reservedCommand(remaining) == ""
	if options.Launch != nil && ordinary {
		if len(remaining) > 0 && remaining[0] == "run" {
			remaining = remaining[1:]
		}
		request := LaunchRequest{Name: options.Name, Module: options.Bundle.Root, StateDir: stateDir,
			Command: *command, Arguments: append([]string(nil), remaining...)}
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
	// Nothing below this point leaves the state directory untouched.
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	unlock, err := lockApplication(stateDir)
	if err != nil {
		if errors.Is(err, errLockBusy) {
			return NewOwnedStateError(err)
		}
		return err
	}
	defer unlock()
	var overrides boot.Config
	if owner.Prepare != nil {
		resources, err := owner.Prepare(ctx)
		if resources.Close != nil {
			defer func() { result = errors.Join(result, resources.Close()) }()
		}
		if err != nil {
			return err
		}
		overrides = resources.Config
	}
	// Preparation runs for as long as the owner needs; the deployment and the
	// runtime process below must not open once the invocation is abandoned.
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := configureDataEnvironment(stateDir, options.DataEnv); err != nil {
		return err
	}
	// The embedded baseline stands in for the activated deployment only when the
	// invocation starts the application; runner commands address the activated one.
	embedded := options.Baseline == baselineEmbedded && reservedCommand(remaining) == ""
	deployment, historyPath, err := selectLaunchDeployment(stateDir, options.Bundle, embedded, base, options.Mode)
	if err != nil {
		return err
	}
	lockPath, err := options.Bundle.Seed(deployment)
	if err != nil {
		return err
	}
	reserved := reservedCommand(remaining)
	if reserved == reservedUpdate {
		if base {
			return NewBaseUpdateRejectedError()
		}
		return updateDeployment(ctx, options, stateDir, deployment, remaining[1:], runChild)
	}
	runtimeArgs := []string{"run", "--silent", "--", command}
	if reserved == reservedRuntime {
		if base {
			return NewBaseRuntimeRejectedError()
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
	return cmd.ExecuteWithOptions(ctx, cmd.ExecuteOptions{
		Args:        runtimeArgs,
		LockFile:    lockPath,
		ConfigFiles: configFiles,
		Components:  options.Components,
		Overrides:   launchOverrides(overrides, historyPath),
	})
}

// validateDataEnvironment accepts only bindings that name an application
// variable and resolve inside the state directory. Every invocation checks the
// executable's declaration, whether or not it goes on to own the state.
func validateDataEnvironment(variables map[string]string) error {
	for _, name := range slices.Sorted(maps.Keys(variables)) {
		relative := variables[name]
		if !environmentName.MatchString(name) || !filepath.IsLocal(relative) || strings.ContainsRune(relative, 0) {
			return NewDataEnvironmentBindingError(name, relative)
		}
	}
	return nil
}

func configureDataEnvironment(state string, variables map[string]string) error {
	if err := validateDataEnvironment(variables); err != nil {
		return err
	}
	for _, name := range slices.Sorted(maps.Keys(variables)) {
		if _, exists := os.LookupEnv(name); !exists {
			if err := os.Setenv(name, filepath.Join(state, variables[name])); err != nil {
				return err
			}
		}
	}
	return nil
}
