// Package terminal2 provides terminal service configuration.
package terminal

import (
	"context"
	"io"

	contextapi "github.com/wippyai/runtime/api/context"
)

// TerminalCtxKey represents the terminal manager context key
var TerminalCtxKey = &contextapi.Key{Name: "terminal"}

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
	if val, ok := fc.Get(TerminalCtxKey); ok {
		if tc, ok := val.(*PipeContext); ok {
			return tc
		}
	}
	return nil
}

// SetTerminalContext sets the terminal context in the given context.
func SetTerminalContext(ctx context.Context, tc *PipeContext) error {
	fc := contextapi.FrameFromContext(ctx)
	if fc == nil {
		return contextapi.ErrNoFrameContext
	}
	return fc.Set(TerminalCtxKey, tc)
}
