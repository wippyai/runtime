package process

import (
	"context"
	"errors"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/internal/closer"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"sync/atomic"
	"time"
)

// Process represents a Lua process instance
type Process struct {
	log      *zap.Logger
	runner   *engine.Runner
	funcName string

	// Context and process information
	ctx    context.Context
	dtt    payload.Transcoder
	cancel context.CancelFunc
	closer *closer.Cleanup
	pid    pubsub.PID

	// State tracking
	pubsub   *subscribe.Layer
	resultCh <-chan engine.Result
	closed   atomic.Bool
}

// NewProcess creates a new Lua process instance
func NewProcess(log *zap.Logger, runner *engine.Runner, funcName string) (process.Process, error) {
	if runner == nil {
		return nil, errors.New("runner is required")
	}

	var pubsubLayer *subscribe.Layer
	for _, layer := range runner.GetLayers() {
		if sl, ok := layer.(*subscribe.Layer); ok {
			pubsubLayer = sl
			break
		}
	}

	if pubsubLayer == nil {
		return nil, errors.New("subscribe layer not found in runner")
	}

	return &Process{
		log:      log,
		runner:   runner,
		funcName: funcName,
		pubsub:   pubsubLayer,
	}, nil
}

// Start initializes and starts the Lua process
func (p *Process) Start(ctx context.Context, pid pubsub.PID, input payload.Payloads) error {
	p.ctx, p.cancel = context.WithCancel(ctx)

	p.pid = pid
	p.dtt = payload.GetTranscoder(ctx)
	if p.dtt == nil {
		return errors.New("failed to get transcoder")
	}

	// Convert input payloads to Lua values
	args, err := p.toLuaPayloads(input)
	if err != nil {
		return err
	}

	ctx = process.WithContext(ctx, &process.Context{
		PID:   pid,
		Start: time.Now(),
		Input: input,
	})

	// Set up runner context and closer
	ctx, p.closer = closer.WithContext(p.runner.WithContext(ctx))

	// Start the Lua function
	p.resultCh, err = p.runner.Start(ctx, p.funcName, args...)
	if err != nil {
		return err
	}

	// Notify that the process has started - do this BEFORE any potential errors
	if onStart := process.GetOnStart(ctx); onStart != nil {
		onStart(pid, p)
	}

	// Handle the initial result if any using select
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	case result := <-p.resultCh:
		if result.Error != nil {
			p.complete(result.Error, result.Result[0])
			return result.Error
		}
		if len(result.Result) > 0 {
			// Process completed immediately
			p.complete(nil, result.Result[0])
			return supervisor.ErrExit
		}
	}

	return nil
}

// Step advances the process state by one iteration
func (p *Process) Step() (bool, error) {
	if p.ctx.Err() != nil || p.closed.Load() {
		return false, p.ctx.Err()
	}

	// Continue the runner
	if err := p.runner.Continue(p.ctx, false); err != nil {
		p.complete(err, nil)
		return false, err
	}

	// Check for any results
	select {
	case result := <-p.resultCh:
		if result.Error != nil {
			p.complete(result.Error, nil)
			return false, result.Error
		}
		if len(result.Result) > 0 {
			p.complete(nil, result.Result[0])
			return false, supervisor.ErrExit
		}
	default:
	}

	return p.runner.HasTasks(), nil
}

// Send handles incoming messages to the process
func (p *Process) Send(ctx context.Context, pkg *pubsub.Package) error {
	if p.ctx.Err() != nil || p.closed.Load() {
		return p.ctx.Err()
	}

	if pkg == nil {
		return errors.New("pkg is nil")
	}

	select {
	case <-p.ctx.Done():
		return p.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	default:
		for _, msg := range pkg.Messages {
			// Forward messages to Lua
			luaValues, err := p.toLuaPayloads(msg.Payloads)
			if err != nil {
				p.log.Error("failed to convert payloads", zap.Error(err))
				continue
			}

			if len(luaValues) > 0 {
				p.pubsub.Publish(msg.Topic, luaValues...)
			}
		}
		return nil
	}
}

// complete handles process completion and cleanup
func (p *Process) complete(err error, result lua.LValue) {
	if p.closed.Swap(true) {
		p.log.Warn("process already completed", zap.String("pid", p.pid.String()))
		return
	}

	if p.closer != nil {
		if cErr := p.closer.Close(); cErr != nil {
			p.log.Error("failed to close resources", zap.Error(cErr))
		}
	}

	if onComplete := process.GetOnComplete(p.ctx); onComplete != nil {
		if err != nil {
			onComplete(p.pid, &runtime.Result{Error: err})
		} else {
			onComplete(p.pid, &runtime.Result{
				Payload: payload.NewPayload(result, payload.Lua),
			})
		}
	}

	p.runner.Close()
	p.cancel()
	p.runner = nil
	p.pid = pubsub.PID{}
}

// toLuaPayloads converts a slice of payloads to Lua values
func (p *Process) toLuaPayloads(payloads payload.Payloads) ([]lua.LValue, error) {
	args := make([]lua.LValue, 0, len(payloads))
	for _, pp := range payloads {
		luaPayload, err := p.dtt.Transcode(pp, payload.Lua)
		if err != nil {
			return nil, err
		}

		if lv, ok := luaPayload.Data().(lua.LValue); ok {
			args = append(args, lv)
		}
	}

	return args, nil
}
