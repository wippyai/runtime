// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/boot"
)

// ErrBusy means another invocation owns the selected application state.
// It says nothing about the owner's identity, readiness or ability to accept clients.
var ErrBusy = errors.New("application state is owned")

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
	Directory string
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
// Config cannot redirect registry history. Deadline optionally bounds execution.
type OwnerResources struct {
	Config   boot.Config
	Close    func() error
	Deadline time.Time
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
			result = errors.Join(result, fmt.Errorf("application launch returned before owner runner completed"))
		}
		mu.Unlock()
		cancel()
		running.Wait()
	}()
	return callback(ctx, request, func(options OwnerOptions) error {
		mu.Lock()
		if closed || started {
			mu.Unlock()
			return fmt.Errorf("application owner runner is single-use and valid only during launch")
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
		if err := ctx.Err(); err != nil {
			return err
		}
		return run(ctx, options)
	})
}

func launchOverrides(config boot.Config, historyPath string) boot.Config {
	sections := make(map[string]map[string]any)
	if config != nil {
		for _, key := range config.Keys() {
			section, name, ok := strings.Cut(key, boot.ConfigSep)
			if !ok {
				continue
			}
			if sections[section] == nil {
				sections[section] = make(map[string]any)
			}
			value, found := config.Get(key)
			if found {
				sections[section][name] = value
			}
		}
	}
	if sections["registry"] == nil {
		sections["registry"] = make(map[string]any)
	}
	sections["registry"]["enable_history"] = true
	sections["registry"]["history_type"] = "sqlite"
	sections["registry"]["history_path"] = historyPath
	options := make([]boot.ConfigOption, 0, len(sections))
	for section, values := range sections {
		options = append(options, boot.WithSection(section, values))
	}
	return boot.NewConfig(options...)
}
