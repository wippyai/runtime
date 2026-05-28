// SPDX-License-Identifier: MPL-2.0

// Package health is a process-wide registry of liveness checks. Boot
// components register a check by name + function; the /livez HTTP
// handler in the prometheus boot component runs all registered checks
// and returns 200 only when all pass.
//
// This exists because TCP-only liveness probes do not detect
// "alive but stuck on the wrong side of partition" — the symptom we
// observed under chaos network-partition (one pod stays Ready while
// making no progress, no broadcasts emitted, peers diverging in
// memory). Activity-based checks (gossip health score, recent raft
// contact, recent PG broadcast) catch this; if any goes stale, kubelet
// kills the pod and reschedules.
//
// Process-wide global state is justified here: liveness is a property
// of the process, and threading a registry through every component's
// boot config would be more boilerplate than it solves.
package health

import (
	"sort"
	"sync"
)

// Check is a registered liveness probe. It returns nil when the
// component is making progress, and an error otherwise. Checks should
// be cheap and non-blocking (no I/O, no locks held > 1ms).
type Check func() error

var (
	mu       sync.RWMutex
	checks   = make(map[string]Check)
	enabled  = true
	disabled = make(map[string]bool)
)

// Register adds (or replaces) a liveness check. The name is reported
// in /livez output so operators can tell which check failed. Calling
// Register with a nil check unregisters the named check.
func Register(name string, c Check) {
	mu.Lock()
	defer mu.Unlock()
	if c == nil {
		delete(checks, name)
		return
	}
	checks[name] = c
}

// Disable suppresses a single check by name. Primarily useful in
// tests that need to assert the handler behavior independent of the
// check implementations. Disabled checks are still listed in /livez
// output as `disabled` so the suppression is visible.
func Disable(name string) {
	mu.Lock()
	defer mu.Unlock()
	disabled[name] = true
}

// Enable removes a Disable for a check name. Safe to call before
// Disable.
func Enable(name string) {
	mu.Lock()
	defer mu.Unlock()
	delete(disabled, name)
}

// SetEnabled toggles the entire registry off (true → off). When off,
// Run reports an empty result and the /livez handler returns 200
// without consulting any checks. Used to silence /livez during
// process teardown so the in-flight last probe doesn't 503 the pod
// while it's already shutting down.
func SetEnabled(on bool) {
	mu.Lock()
	defer mu.Unlock()
	enabled = on
}

// Result captures the outcome of a single check.
type Result struct {
	Err  error
	Name string
}

// Run executes every registered check and returns a deterministic-
// order slice of results. Order is alphabetical so /livez output is
// stable across calls.
func Run() []Result {
	mu.RLock()
	if !enabled {
		mu.RUnlock()
		return nil
	}
	names := make([]string, 0, len(checks))
	for n := range checks {
		names = append(names, n)
	}
	mu.RUnlock()
	sort.Strings(names)

	out := make([]Result, 0, len(names))
	for _, n := range names {
		mu.RLock()
		c := checks[n]
		isDisabled := disabled[n]
		mu.RUnlock()
		if isDisabled {
			out = append(out, Result{Name: n, Err: errDisabled})
			continue
		}
		if c == nil {
			continue
		}
		err := safeRun(c)
		out = append(out, Result{Name: n, Err: err})
	}
	return out
}

// safeRun shields the registry from a panicking check. A panicking
// check counts as a failure (the panic value is wrapped in an error)
// rather than crashing the /livez handler goroutine.
func safeRun(c Check) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errCheckPanic
		}
	}()
	return c()
}
