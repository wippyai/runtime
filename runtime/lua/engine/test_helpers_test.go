// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

// mustNewProcess creates a new process and fails the test if it errors.
// Use this in tests where process creation should never fail.
func mustNewProcess(t testing.TB, opts ...ProcessOption) *Process {
	t.Helper()
	proc, err := NewProcess(opts...)
	if err != nil {
		t.Fatalf("NewProcess failed: %v", err)
	}
	return proc
}

// wrapBinder converts a simple binder func to ModuleBinder.
// Use this to adapt old-style test binders that don't return errors.
func wrapBinder(fn func(*lua.LState)) ModuleBinder {
	return func(l *lua.LState) error {
		fn(l)
		return nil
	}
}
