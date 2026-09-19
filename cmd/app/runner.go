// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
	"github.com/wippyai/runtime/cmd/wippy/cmd"
)

// execute runs the Wippy CLI for one invocation. It owns the process command
// state, so an invocation reaches it once. Tests replace it to observe the
// options an operation selects.
var execute = cmd.ExecuteWithOptions

// readOnlyCommands are the Wippy CLI commands that write nothing under the
// deployment or the state directory, so they run while another invocation owns
// the state: version and help print, search and readme read Hub metadata
// through the machine-wide credential store, and lint loads the deployment
// from its lock without installing modules and keeps compiled code in the
// machine-wide, toolchain-keyed store under internal/cachedir.
var readOnlyCommands = map[string]bool{
	"lint":    true,
	"version": true,
	"help":    true,
	"search":  true,
	"readme":  true,
}

// readOnlyWippy reports whether a Wippy CLI invocation leaves the deployment
// and the state untouched. The bare CLI prints its usage.
func readOnlyWippy(args []string) bool {
	if len(args) == 0 {
		return true
	}
	return readOnlyCommands[args[0]]
}

// operate carries out one launch. Everything below this point works inside the
// state directory, so an abandoned invocation stops before it opens anything.
func operate(ctx context.Context, e Executable, l Launch, prepare func(context.Context) (boot.Config, func() error, error)) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(l.State, 0o700); err != nil {
		return NewApplicationStateError("create application state", l.State, err)
	}
	if l.Op != OpWippy || !readOnlyWippy(l.Args) {
		unlock, err := lockState(l.State)
		if err != nil {
			return err
		}
		defer func() { result = errors.Join(result, unlock()) }()
	}
	if err := applyData(l.State, e.Data); err != nil {
		return err
	}
	var hosted boot.Config
	if prepare != nil {
		config, release, err := prepare(ctx)
		if release != nil {
			defer func() { result = errors.Join(result, release()) }()
		}
		if err != nil {
			return err
		}
		hosted = config
	}
	// Preparation runs for as long as the host needs, and the deployment below
	// opens only while the invocation is still wanted.
	if err := ctx.Err(); err != nil {
		return err
	}
	deployment, err := selectDeployment(e, l)
	if err != nil {
		return err
	}
	lockPath, err := e.Bundle.Seed(deployment)
	if err != nil {
		return err
	}
	if l.Op == OpUpdate {
		return updateDeployment(ctx, e, l, deployment, runChild)
	}
	skipped, err := seedCache(l.State, deployment, e.Bundle)
	if err != nil {
		return err
	}
	for _, failure := range skipped {
		// A retained deployment only shortens startup. The launch continues on
		// the deployments that remain readable, and each skipped source is
		// named so the state directory can be repaired.
		fmt.Fprintf(os.Stderr, "%s: retained deployment skipped while seeding the artifact cache: %v\n", e.Name, failure)
	}
	if l.Op == OpRecover {
		if err := recordRecovery(e, l, deployment); err != nil {
			return err
		}
	}
	files, err := configFiles(l.State)
	if err != nil {
		return err
	}
	return execute(ctx, cmd.ExecuteOptions{
		Args:        runtimeArgs(l),
		LockFile:    lockPath,
		ConfigFiles: files,
		Components:  e.Components,
		Overrides:   pin(hosted, historyFor(l), cachePath(l.State)),
	})
}

// runtimeArgs is the CLI command line an operation runs. The Wippy operation
// passes its arguments through; every other operation starts the application
// command the launch selected.
func runtimeArgs(l Launch) []string {
	if l.Op == OpWippy {
		return l.Args
	}
	return append([]string{"run", "--silent", "--", l.Command}, l.Args...)
}

// pin layers the host's configuration under the settings the runner owns, so
// the registry history and the artifact cache stay where the state holds them.
func pin(config boot.Config, history, cache string) boot.Config {
	return bootconfig.Merge(config, boot.NewConfig(boot.WithSection("registry", map[string]any{
		"enable_history":        true,
		"history_type":          "sqlite",
		"history_path":          history,
		"dependency_vendor_dir": cache,
	})))
}
