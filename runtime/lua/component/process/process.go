package process

import (
	"context"
	"errors"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"sync"
	"sync/atomic"
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
	uow    engine.UnitOfWork
	pid    pubsub.PID

	// State tracking
	wg       sync.WaitGroup
	resultCh <-chan *engine.Update
	closed   atomic.Bool
}

// NewProcess creates a new Lua process instance
func NewProcess(log *zap.Logger, runner *engine.Runner, funcName string) (process.Process, error) {
	if runner == nil {
		return nil, errors.New("runner is required")
	}

	return &Process{
		log:      log,
		runner:   runner,
		funcName: funcName,
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

	ctx = pubsub.WithPID(ctx, pid)

	// Set up runner context and uow
	p.uow, p.ctx = p.runner.InitUnitOfWork(ctx)

	// Start the Lua function
	p.resultCh, err = p.runner.Start(p.ctx, p.funcName, args...)
	if err != nil {
		return err
	}

	// Notify that the process has started - do this BEFORE any potential errors
	if onStart := process.GetOnStart(p.ctx); onStart != nil {
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
func (p *Process) Step() error {
	p.wg.Add(1)

	if p.ctx.Err() != nil || p.closed.Load() {
		p.wg.Done()
		return p.ctx.Err()
	}

	// Continue the runner
	if err := p.runner.Continue(p.ctx, false); err != nil {
		p.wg.Done()
		p.complete(err, nil)
		return err
	}

	// Check for any results
	select {
	case result := <-p.resultCh:
		if result.Error != nil {
			p.wg.Done()
			p.complete(result.Error, nil)
			return result.Error
		}
		if len(result.Result) > 0 {
			p.wg.Done()
			p.complete(nil, result.Result[0])
			return supervisor.ErrExit
		}
	default:
	}

	p.wg.Done()
	return nil
}

// Ready returns the size of the runner's queue that is ready to be processed.
func (p *Process) Ready() int {
	p.wg.Add(1)
	defer p.wg.Done()

	return p.uow.Tasks().Ready() + p.runner.QueueLen()
}

// Send handles incoming messages to the process
func (p *Process) Send(pkg *pubsub.Package) error {
	p.wg.Add(1)
	defer p.wg.Done()

	if p.ctx.Err() != nil || p.closed.Load() {
		return p.ctx.Err()
	}

	if pkg == nil {
		return errors.New("pkg is nil")
	}

	select {
	case <-p.ctx.Done():
		return p.ctx.Err()
	default:
		for _, msg := range pkg.Messages {
			// Convert payloads to Lua values
			luaValues, err := p.toLuaPayloads(msg.Payloads)
			if err != nil {
				p.log.Error("failed to convert payloads", zap.Error(err))
				continue
			}

			if len(luaValues) == 0 {
				continue
			}

			if exists, _ := subscribe.Exists(p.ctx, msg.Topic); exists {
				err := subscribe.Publish(p.ctx, msg.Topic, luaValues...)
				if err != nil {
					p.log.Error("failed to publish message",
						zap.String("topic", msg.Topic),
						zap.Error(err))
				}
				continue
			}

			// Fallback to inbox if available
			inboxValues := make([]lua.LValue, 0, len(luaValues))

			state := p.uow.State()

			// Create a message table for each value
			for _, v := range luaValues {
				msgTable := state.CreateTable(0, 2)
				msgTable.RawSetString("topic", lua.LString(msg.Topic))
				msgTable.RawSetString("payload", v) // todo: raw payload?

				inboxValues = append(inboxValues, msgTable)
			}

			if pErr := subscribe.Publish(p.ctx, topology.TopicInbox, inboxValues...); pErr != nil {
				p.log.Error("failed to publish message",
					zap.String("topic", topology.TopicInbox),
					zap.Error(pErr))
			}
		}
		pubsub.ReleasePackage(pkg)
		return nil
	}
}

// complete handles process completion and cleanup
func (p *Process) complete(err error, result lua.LValue) {
	if p.closed.Swap(true) {
		p.log.Warn("process already completed", zap.String("pid", p.pid.String()))
		return
	}

	p.wg.Wait()

	if p.uow != nil {
		if cErr := p.uow.Close(); cErr != nil {
			p.log.Error("failed to close unit of work", zap.Error(cErr))
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
	p.uow = nil
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
