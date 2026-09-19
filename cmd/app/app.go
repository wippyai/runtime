// SPDX-License-Identifier: MPL-2.0

// Package app runs a native executable built on the Wippy runtime.
//
// An executable declares the packs it ships, the application command it starts
// and the state it owns. Its arguments are:
//
//	<exe> [--state DIR] run     <app args...>    ordinary start
//	<exe> [--state DIR] update  <hub args...>    move the deployment forward
//	<exe> [--state DIR] recover                  boot the shipped packs afresh
//	<exe> [--state DIR] wippy   <cli args...>    the Wippy CLI on the deployment
//
// run, update, recover and wippy are reserved as the first argument by design.
// A bare invocation, or one whose first word is none of the four, selects run
// and passes every argument to the application. --state is the only host flag
// and precedes the verb; everything after the verb belongs to the verb.
package app

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"

	"github.com/wippyai/runtime/api/boot"
)

// Executable is the complete declaration of a native Wippy application.
// Data maps application-owned environment variables to paths inside the state
// directory; a variable already present in the environment is a user override
// and keeps its value.
type Executable struct {
	Data       map[string]string
	Host       Host
	Name       string
	Command    string
	Bundle     Bundle
	Components []boot.Component
}

var applicationName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
var environmentName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// Main runs the executable with the process arguments under a context that
// ends on os.Interrupt or SIGTERM, reports a failure on stderr and exits 1.
func Main(e Executable) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := Run(ctx, e, os.Args[1:]); err != nil {
		stop()
		fmt.Fprintf(os.Stderr, "%s: %v\n", e.Name, err)
		os.Exit(1)
	}
}

// Run executes one invocation of the executable. It validates the declaration,
// parses the argument grammar, gives the host its plan and then carries out the
// selected operation.
func Run(ctx context.Context, e Executable, args []string) error {
	if err := e.validate(); err != nil {
		return err
	}
	launch, err := parseLaunch(e, args)
	if err != nil {
		return err
	}
	var prepare func(context.Context) (boot.Config, func() error, error)
	if e.Host != nil {
		plan, err := e.Host.Plan(ctx, launch)
		if err != nil {
			return err
		}
		if plan.State != "" {
			state, err := filepath.Abs(plan.State)
			if err != nil {
				return NewApplicationStateError("resolve planned state", plan.State, err)
			}
			launch.State = state
		}
		if plan.Command != "" {
			launch.Command = plan.Command
		}
		if plan.Args != nil {
			launch.Args = plan.Args
		}
		if plan.Run != nil {
			return plan.Run(ctx)
		}
		prepare = plan.Prepare
	}
	return operate(ctx, e, launch, prepare)
}

// validate checks the declaration on every invocation, before the runner
// touches the state directory. The bundle is validated by Seed, which is the
// point at which its contents are written.
func (e Executable) validate() error {
	if !applicationName.MatchString(e.Name) {
		return NewInvalidApplicationNameError(e.Name)
	}
	if e.Command == "" {
		return NewMissingApplicationCommandError()
	}
	for _, name := range slices.Sorted(maps.Keys(e.Data)) {
		path := e.Data[name]
		if !environmentName.MatchString(name) || !filepath.IsLocal(path) || strings.ContainsRune(path, 0) {
			return NewDataEnvironmentBindingError(name, path)
		}
	}
	return nil
}

// applyData binds each declared variable to its path inside state. A variable
// the environment already carries stays as the user set it.
func applyData(state string, data map[string]string) error {
	for _, name := range slices.Sorted(maps.Keys(data)) {
		if _, exists := os.LookupEnv(name); exists {
			continue
		}
		if err := os.Setenv(name, filepath.Join(state, data[name])); err != nil {
			return NewApplicationStateError("bind data environment "+name, state, err)
		}
	}
	return nil
}
