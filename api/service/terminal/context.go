// Package terminal provides terminal service configuration.
package terminal

import (
	"context"
	"io"

	contextapi "github.com/wippyai/runtime/api/context"
)

var terminalKey = &contextapi.Key{Name: "terminal"}

// Key returns the context key for terminal context.
func Key() *contextapi.Key {
	return terminalKey
}

// PipeContext holds the standard input/output/error streams for terminal operations.
type PipeContext struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Args   []string
}

// NewTerminalContext creates a new terminal context with the provided input/output streams.
func NewTerminalContext(stdin io.Reader, stdout, stderr io.Writer) *PipeContext {
	return &PipeContext{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	}
}

// NewTerminalContextWithArgs creates a terminal context with args.
func NewTerminalContextWithArgs(stdin io.Reader, stdout, stderr io.Writer, args []string) *PipeContext {
	return &PipeContext{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Args:   args,
	}
}

// GetTerminalContext retrieves the terminal context from the given context if available.
func GetTerminalContext(ctx context.Context) *PipeContext {
	fc := contextapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(terminalKey); ok {
		if tc, ok := val.(*PipeContext); ok {
			return tc
		}
	}
	return nil
}

// WithTerminalContext sets the terminal context in the given context.
func WithTerminalContext(ctx context.Context, tc *PipeContext) error {
	fc := contextapi.FrameFromContext(ctx)
	if fc == nil {
		return contextapi.ErrNoFrameContext
	}
	return fc.Set(terminalKey, tc)
}
