package terminal

import (
	"context"
	"io"

	contextapi "github.com/ponyruntime/pony/api/context"
)

// terminalCtx represents the terminal manager context key
var terminalCtx = &contextapi.Key{Name: "terminal"}

type PipeContext struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func NewTerminalContext(stdin io.Reader, stdout, stderr io.Writer) *PipeContext {
	return &PipeContext{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	}
}

func GetTerminalContext(ctx context.Context) *PipeContext {
	fc := contextapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(terminalCtx); ok {
		if tc, ok := val.(*PipeContext); ok {
			return tc
		}
	}
	return nil
}

func SetTerminalContext(ctx context.Context, tc *PipeContext) error {
	fc := contextapi.FrameFromContext(ctx)
	if fc == nil {
		return contextapi.ErrNoFrameContext
	}
	return fc.Set(terminalCtx, tc)
}
