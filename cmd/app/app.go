// SPDX-License-Identifier: MPL-2.0

// Package app runs a native executable built on the Wippy runtime.
//
// An executable declares the packs it ships, the application command it starts
// and the state it owns. Its arguments are:
//
//	<exe> [--state DIR] run     <app args...>    ordinary start
//	<exe> [--state DIR] update  <hub args...>    move the deployment forward
//	<exe> [--state DIR] recover <app args...>    boot the shipped packs afresh
//	<exe> [--state DIR] wippy   <cli args...>    the Wippy CLI on the deployment
//
// run, update, recover and wippy are reserved as the first argument by design.
// A bare invocation, or one whose first word is none of the four, selects run
// and passes every argument to the application. --state DIR or --state=DIR is
// the only argument the runner reads and it precedes the verb; everything
// after the verb belongs to the verb, including an argument shaped like a
// flag. Owned reports whether a state directory has an owner right now.
//
// State selection precedes boot configuration: an explicit --state wins over
// the host plan's DefaultState, then Executable.State, then the user's
// configuration directory. Boot continues through the normal boot.Config
// path, using published pack defaults, the selected state's .wippy.yaml,
// profiles and runtime overrides.
//
// OwnedCommand is chosen only when the actual state lock is busy. It starts
// the application's command in private temporary state seeded from the shipped
// bundle, without inheriting the owner's .wippy.yaml, history, data bindings
// or selected deployment. Host preparation and normal runtime overrides still
// apply; the application implements any connection to the owner itself.
package app

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/wippyai/runtime/api/boot"
)

// Executable is the complete declaration of a native Wippy application.
//
//	Name       addresses the default state directory, ^[a-z][a-z0-9_-]*$
//	Command    the application command an ordinary start runs
//	Bundle     the packs the executable ships, immutable
//	Data       application-owned environment variables bound to paths inside
//	           the state directory; a variable the environment already carries
//	           is a user override and keeps its value
//	Components the native components the host adds to the runtime
//	Host       decides what an invocation does, optional
//
// The declaration order below follows the memory layout the runtime pins.
type Executable struct {
	Data         map[string]string
	Host         Host
	LuaCacheSeed *LuaCacheSeed
	Name         string
	Command      string
	Components   []boot.Component
	// State is the default state directory when the invocation names none. A
	// relative path resolves against the working directory, so each folder
	// holds its own state. Empty selects the user configuration directory.
	State string
	// OwnedCommand, when set, runs that application command in a transient
	// state if acquiring the selected state's lock returns ErrOwned. It applies
	// only to run, after host planning; a plan's Run or Transient takes precedence.
	// Empty preserves the ordinary refusal. No owner readiness or IPC is implied.
	OwnedCommand string
	Bundle       Bundle
}

var applicationName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
var environmentName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// Main runs the executable with the process arguments. Until the runner
// hands the process to the Wippy CLI, os.Interrupt and SIGTERM cancel the
// invocation; afterwards the CLI owns them. A failure is reported on stderr
// and exits 1.
func Main(e Executable) {
	ctx, signals := captureSignals(context.Background())
	defer signals.close()
	if err := Run(ctx, e, os.Args[1:]); err != nil {
		signals.close()
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
		if err := plan.validate(launch); err != nil {
			return err
		}
		if !launch.Explicit && plan.DefaultState != "" {
			state, err := resolveDefaultState(launch.Dir, plan.DefaultState)
			if err != nil {
				return NewApplicationStateError("resolve planned state", plan.DefaultState, err)
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
		if plan.Transient {
			return operateTransient(ctx, e, launch, prepare)
		}
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
	if strings.ContainsRune(e.State, 0) {
		return NewInvalidApplicationStateError()
	}
	if strings.ContainsRune(e.OwnedCommand, 0) {
		return NewInvalidOwnedCommandError()
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
