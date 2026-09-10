// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/cmd/wippy/cmd"
)

// Options selects the embedded application and its native host components.
// DataEnv maps application-owned environment variables to paths within StateDir.
// Existing environment values remain explicit user overrides.
type Options struct {
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
	if !applicationName.MatchString(options.Name) {
		return fmt.Errorf("invalid application name")
	}
	if options.Mode != "base" && options.Mode != "bootstrap" {
		return fmt.Errorf("application mode must be base or bootstrap")
	}
	if options.Command == "" {
		return fmt.Errorf("application command is required")
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
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	if err := configureDataEnvironment(stateDir, options.DataEnv); err != nil {
		return err
	}
	unlock, err := lockApplication(stateDir)
	if err != nil {
		return err
	}
	defer unlock()
	deployment, err := selectedDeployment(stateDir)
	if err != nil {
		return err
	}
	historyPath := filepath.Join(stateDir, "registry.db")
	if *base {
		if options.Mode != "base" {
			return fmt.Errorf("bootstrap applications do not expose a base deployment")
		}
		// Each executable's embedded content selects an independent baseline.
		// Application databases are intentionally not rolled back or removed.
		deployment = filepath.Join(stateDir, "base", bundleID(options.Bundle))
		historyPath = filepath.Join(deployment, "registry.db")
	}
	lockPath, err := options.Bundle.Seed(deployment)
	if err != nil {
		return err
	}
	remaining := flags.Args()
	if len(remaining) > 0 && remaining[0] == "update" {
		if *base {
			return fmt.Errorf("base recovery cannot be updated")
		}
		return updateDeployment(ctx, options, stateDir, deployment, remaining[1:], runChild)
	}
	runtimeArgs := []string{"run", "--silent", "--", *command}
	if len(remaining) > 0 && remaining[0] == "runtime" {
		if *base {
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
	return cmd.ExecuteWithOptions(ctx, cmd.ExecuteOptions{
		Args:        runtimeArgs,
		LockFile:    lockPath,
		ConfigFiles: configFiles,
		Components:  options.Components,
		Overrides: boot.NewConfig(boot.WithSection("registry", map[string]any{
			"enable_history": true,
			"history_type":   "sqlite",
			"history_path":   historyPath,
		})),
	})
}

func configureDataEnvironment(state string, variables map[string]string) error {
	for name, relative := range variables {
		if !environmentName.MatchString(name) || !filepath.IsLocal(relative) {
			return fmt.Errorf("invalid application data environment binding %q", name)
		}
		if _, exists := os.LookupEnv(name); !exists {
			if err := os.Setenv(name, filepath.Join(state, relative)); err != nil {
				return err
			}
		}
	}
	return nil
}
