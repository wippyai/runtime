// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
	"github.com/wippyai/runtime/cmd/wippy/cmd"
)

// execute runs the Wippy CLI for one invocation. It owns the process command
// state, so an invocation reaches it once. Tests replace it to observe the
// options an operation selects.
var execute = cmd.ExecuteWithOptions

// stateFreeCommands do not require a deployment. They can run while another
// invocation owns the state, without seeding a bundle or its artifact cache.
// Lint needs a deployment and therefore takes the state lock.
var stateFreeCommands = map[string]bool{
	"version": true,
	"help":    true,
	"search":  true,
	"readme":  true,
}

// stateFreeWippy reports whether a CLI invocation can run without a deployment.
// The bare CLI prints its usage.
func stateFreeWippy(args []string) bool {
	if len(args) == 0 {
		return true
	}
	return stateFreeCommands[args[0]]
}

// operateStateFree never creates the state or seeds the bundle. ExecuteWithOptions
// requires a lock path, but these commands never open it.
func operateStateFree(ctx context.Context, e Executable, l Launch) error {
	files, err := configFiles(l.State)
	if err != nil {
		return err
	}
	return execute(ctx, cmd.ExecuteOptions{
		Args:        l.Args,
		LockFile:    filepath.Join(deploymentsPath(l.State), e.Bundle.ID(), lock.DefaultFilename),
		ConfigFiles: files,
		Components:  e.Components,
	})
}

// operate carries out one launch. Everything below this point works inside the
// state directory, so an abandoned invocation stops before it opens anything.
func operate(ctx context.Context, e Executable, l Launch, prepare func(context.Context) (boot.Config, func() error, error)) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if l.Op == OpWippy && stateFreeWippy(l.Args) && prepare == nil {
		return operateStateFree(ctx, e, l)
	}
	if err := os.MkdirAll(l.State, 0o700); err != nil {
		return NewApplicationStateError("create application state", l.State, err)
	}
	unlock, err := lockState(l.State)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, unlock()) }()
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
		return updateDeployment(ctx, e, l, deployment, childRunner)
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
	history := historyPath(l.State)
	if l.Op == OpRecover {
		history, err = recordRecovery(e, l, deployment)
		if err != nil {
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
		Overrides:   pin(hosted, history, cachePath(l.State)),
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
