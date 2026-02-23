// SPDX-License-Identifier: MPL-2.0

package cli

import (
	"context"
	"fmt"
)

const (
	// ExitNamespace exposes WASI preview2 CLI exit API.
	ExitNamespace = "wasi:cli/exit@0.2.3"
)

// ExitError represents guest-requested wasi:cli/exit invocation.
type ExitError struct {
	Status uint32
}

func (e ExitError) Error() string {
	return fmt.Sprintf("wasi exit requested: status %d", e.Status)
}

// ExitHost prevents process-wide os.Exit; guest exit is surfaced as trap/panic.
type ExitHost struct{}

// NewExitHost builds a safe WASI CLI exit host.
func NewExitHost() *ExitHost {
	return &ExitHost{}
}

// Namespace implements wasm-runtime Host.
func (h *ExitHost) Namespace() string {
	return ExitNamespace
}

// Exit traps execution with ExitError instead of terminating host process.
func (h *ExitHost) Exit(_ context.Context, status uint32) {
	panic(ExitError{Status: status})
}
