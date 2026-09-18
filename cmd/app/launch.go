// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"errors"
	"sync"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
)

// Launch runs an application-specific invocation before owner state is opened.
// It may run a client without calling runOwner. The callback owns discovery,
// authentication and admission; the runner never attaches or retries for it.
// Update, runtime tooling and --base bypass this callback.
// runOwner is single-use, synchronous, and invalid after Launch returns.
type Launch func(ctx context.Context, request LaunchRequest, runOwner func(OwnerOptions) error) error

// LaunchRequest identifies the ordinary invocation. StateDir is already resolved
// from --state-dir or the executable's default and cannot be redirected by runOwner.
type LaunchRequest struct {
	Name      string
	Module    string
	StateDir  string
	Command   string
	Arguments []string
}

// OwnerOptions selects the application command and preparation for one owner run.
// Empty Command and nil Arguments retain the invocation values. Prepare runs
// under exclusive state ownership, before deployment or application stores open.
type OwnerOptions struct {
	Prepare   func(context.Context) (OwnerResources, error)
	Command   string
	Arguments []string
}

// OwnerResources lives inside the owner's exclusive lifetime. Close runs after
// runtime shutdown and before unlocking, even when preparation or startup fails.
// Config cannot redirect registry history.
type OwnerResources struct {
	Config boot.Config
	Close  func() error
}

func launch(ctx context.Context, callback Launch, request LaunchRequest, run func(context.Context, OwnerOptions) error) (result error) {
	ctx, cancel := context.WithCancel(ctx)
	var mu sync.Mutex
	var running sync.WaitGroup
	var started, closed, active bool
	defer func() {
		mu.Lock()
		closed = true
		if active {
			result = errors.Join(result, NewLaunchIncompleteError())
		}
		mu.Unlock()
		cancel()
		running.Wait()
	}()
	return callback(ctx, request, func(options OwnerOptions) error {
		mu.Lock()
		if closed || started {
			mu.Unlock()
			return NewOwnerRunnerReusedError()
		}
		started = true
		active = true
		running.Add(1)
		mu.Unlock()
		defer func() {
			mu.Lock()
			active = false
			mu.Unlock()
			running.Done()
		}()
		return run(ctx, options)
	})
}

// launchOverrides layers the owner's settings under the registry history the
// runner pins to the selected state, so no owner key can redirect history.
func launchOverrides(config boot.Config, historyPath string) boot.Config {
	return bootconfig.Merge(config, boot.NewConfig(boot.WithSection("registry", map[string]any{
		"enable_history": true,
		"history_type":   "sqlite",
		"history_path":   historyPath,
	})))
}
