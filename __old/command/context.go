package command

import (
	"container/list"
	"context"
	"errors"
	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
	"log"
	"sync/atomic"
)

// Define context key for command layer
var cmdContext = ctxapi.Key{Name: "command.context"}

var (
	ErrCommandCanceled      = errors.New("command canceled")
	ErrNoUnitOfWork         = errors.New("no unit of work found")
	ErrLayerContextNotFound = errors.New("layer context not found in unit of work")
	commandCounter          atomic.Uint64
)

// layerContext maintains state for command operations
type layerContext struct {
	queue *list.List // Commands waiting to be processed
}

// getLayerContext retrieves the command layer context from the UnitOfWork
func getLayerContext(uw engine.UnitOfWork) *layerContext {
	ctx, ok := uw.Values().Get(cmdContext)
	if !ok {
		return nil
	}

	if v, ok := ctx.(*layerContext); ok {
		return v
	}

	return nil
}

// ensureLayerContext gets or creates a command layer context in the UnitOfWork
func ensureLayerContext(uw engine.UnitOfWork) *layerContext {
	if uw == nil {
		return nil
	}

	ctx, ok := uw.Values().Get(cmdContext)
	if !ok {
		ctx = &layerContext{queue: list.New()}
		uw.Values().Set(cmdContext, ctx)
		return ctx.(*layerContext)
	}

	if v, ok := ctx.(*layerContext); ok {
		return v
	}

	return nil
}

// Schedule adds a command to the queue queue
func Schedule(ctx context.Context, cmd *Command) error {
	uw := engine.GetUnitOfWork(ctx)
	if uw == nil {
		return ErrNoUnitOfWork
	}

	lCtx := ensureLayerContext(uw)
	if lCtx == nil {
		return ErrLayerContextNotFound
	}

	lCtx.queue.PushBack(cmd)

	return nil
}

// Result marks a command as completed with the given result
func Result(ctx context.Context, cmd *Command, result lua.LValue) error {
	uw := engine.GetUnitOfWork(ctx)
	if uw == nil {
		return ErrNoUnitOfWork
	}

	lCtx := ensureLayerContext(uw)
	if lCtx == nil {
		return ErrLayerContextNotFound
	}

	if !cmd.IsComplete() {
		cmd.SetResult(result)
		return respond(uw, cmd)
	}

	return nil
}

// Error marks a command as failed with the given error
func Error(ctx context.Context, cmd *Command, err error) error {
	uw := engine.GetUnitOfWork(ctx)
	if uw == nil {
		return ErrNoUnitOfWork
	}

	lCtx := ensureLayerContext(uw)
	if lCtx == nil {
		return ErrLayerContextNotFound
	}

	if !cmd.IsComplete() {
		cmd.SetError(err)
		return respond(uw, cmd)
	}
	return nil
}

// FlushQueue returns all queue commands and clears the queue
func FlushQueue(ctx context.Context) ([]*Command, error) {
	uw := engine.GetUnitOfWork(ctx)
	if uw == nil {
		return nil, ErrNoUnitOfWork
	}

	lCtx := getLayerContext(uw)
	if lCtx == nil {
		return nil, ErrLayerContextNotFound
	}

	var commands []*Command
	for e := lCtx.queue.Front(); e != nil; e = e.Next() {
		if cmd, ok := e.Value.(*Command); ok {
			commands = append(commands, cmd)
		}
	}
	lCtx.queue.Init() // Clear processed

	return commands, nil
}

func respond(uw engine.UnitOfWork, cmd *Command) error {
	return uw.Tasks().Schedule(func() {
		if cmd.err != nil {
			if err := channel.Close(uw.State(), cmd.response); err != nil {
				log.Printf("error closing channel: %v", err)
			}
		} else {
			if err := channel.Send(uw.State(), cmd.response, cmd.result); err != nil {
				log.Printf("error sending result: %v", err)
			}

			if err := channel.Close(uw.State(), cmd.response); err != nil {
				log.Printf("error closing channel: %v", err)
			}
		}
	})
}
