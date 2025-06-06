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
	if tc, ok := ctx.Value(terminalCtx).(*PipeContext); ok {
		return tc
	}
	return nil
}

func WithTerminalContext(ctx context.Context, tc *PipeContext) context.Context {
	return context.WithValue(ctx, terminalCtx, tc)
}
