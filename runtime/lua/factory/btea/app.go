package btea

import (
	"context"
	"errors"
	"github.com/ponyruntime/pony/internal/closer"
	"github.com/ponyruntime/pony/runtime/lua/modules/tasks"
	"sync"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	"github.com/ponyruntime/pony/runtime/lua/modules/upstream"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

const ChannelInbox = "@btea/inbox"

// Process implements process.Process with runner management
type Process struct {
	mu       sync.RWMutex
	log      *zap.Logger
	dtt      payload.Transcoder
	runner   *engine.Runner
	funcName string
	pubsub   *subscribe.Layer
	upstream chan payload.Payload
	done     chan struct{}
	ctx      context.Context
	pid      process.PID
	stepErr  error
	closer   *closer.Cleanup
}

// NewBteaProcess constructs a new Process instance
func NewBteaProcess(
	log *zap.Logger,
	dtt payload.Transcoder,
	runner *engine.Runner,
	funcName string,
) (process.Process, error) {
	if log == nil {
		log = zap.NewNop()
	}

	if dtt == nil {
		return nil, errors.New("transcoder is required")
	}

	if runner == nil {
		return nil, errors.New("runner is required")
	}

	var subLayer *subscribe.Layer
	for _, layer := range runner.GetLayers() {
		if sl, ok := layer.(*subscribe.Layer); ok {
			subLayer = sl
			break
		}
	}

	if subLayer == nil {
		return nil, errors.New("subscribe layer not found in runner")
	}

	return &Process{
		log:      log,
		dtt:      dtt,
		runner:   runner,
		funcName: funcName,
		pubsub:   subLayer,
		upstream: make(chan payload.Payload, 100),
		done:     make(chan struct{}),
	}, nil
}

// Start initializes the process
func (p *Process) Start(ctx context.Context, pid process.PID, input payload.Payloads) error {
	p.ctx = ctx
	p.pid = pid

	ctx = upstream.WithUpstreamChannel(ctx, p.upstream)
	ctx = p.runner.WithContext(ctx)
	ctx, p.closer = closer.WithContext(ctx)

	resultCh, err := p.runner.Start(ctx, p.funcName, getLuaArgs(input)...)
	if err != nil {
		return err
	}

	if onStart := process.GetOnStart(ctx); onStart != nil {
		onStart(pid, p)
	}

	go func() {
		defer func() {
			close(p.done)
			close(p.upstream)
			p.runner.Close()
		}()

		completeOnce := sync.Once{}

		for {
			select {
			case result := <-resultCh:
				if result.Error != nil {
					p.log.Error("runner error", zap.Error(result.Error))
					completeOnce.Do(func() {
						if cErr := p.closer.Close(); cErr != nil {
							p.log.Error("failed to close resources", zap.Error(cErr))
						}
						if onComplete := process.GetOnComplete(p.ctx); onComplete != nil {
							onComplete(p.pid, &runtime.Result{Error: result.Error})
						}
					})
					return
				}
				if len(result.Result) > 0 {
					p.log.Debug("runner completed", zap.Any("result", result.Result[0]))
					completeOnce.Do(func() {
						if cErr := p.closer.Close(); cErr != nil {
							p.log.Error("failed to close resources", zap.Error(cErr))
						}
						if onComplete := process.GetOnComplete(p.ctx); onComplete != nil {
							onComplete(p.pid, &runtime.Result{
								Payload: payload.NewPayload(result.Result[0], payload.Lua),
							})
						}
					})
					return
				}
			case msg := <-p.upstream:
				p.log.Debug("received upstream message", zap.Any("msg", msg))
			case <-ctx.Done():
				err := ctx.Err()
				if p.stepErr != nil {
					err = p.stepErr
				}
				completeOnce.Do(func() {
					if cErr := p.closer.Close(); cErr != nil {
						p.log.Error("failed to close resources", zap.Error(cErr))
					}
					if onComplete := process.GetOnComplete(p.ctx); onComplete != nil {
						onComplete(p.pid, &runtime.Result{Error: err})
					}
				})
				return
			}
		}
	}()

	go func() {
		for {
			select {
			case <-p.done:
				return
			case <-time.After(1 * time.Second):
				task, err := tasks.CreateTask(payload.NewPayload(lua.LString("hello world"), payload.Lua))
				if err != nil {
					p.log.Error("failed to create task",
						zap.Error(err))
					continue
				}

				p.pubsub.Publish(ChannelInbox, tasks.WrapTask(p.runner.GetCVM().State(), task))

				select {
				case <-p.done:
				case rsp := <-task.Response:

					p.log.Debug("received task response", zap.Any("rsp", rsp.Data()))
				}
			}
		}
	}()

	return nil
}

// Step updates the process state
func (p *Process) Step() error {
	select {
	case <-p.done:
		return nil
	default:
		err := p.runner.Continue(p.ctx)
		if err != nil {
			p.stepErr = err
		}

		return err
	}
}

// Send routes messages to the process and publish to subscribers
func (p *Process) Send(msg ...*process.Message) error {
	select {
	case <-p.done:
		return errors.New("process stopped")
	default:
		for _, m := range msg {
			if m.Topic == process.TopicCancel {
				p.pubsub.Release(m.Topic) // we want all subscribers to be released at the same time
				continue
			}

			luaValues := make([]lua.LValue, 0, len(m.Payloads))
			for _, pp := range m.Payloads {
				luaPayload, err := p.dtt.Transcode(pp, payload.Lua)
				if err != nil {
					p.log.Error("failed to transcode payload",
						zap.Error(err),
						zap.String("topic", m.Topic))
					continue
				}
				if luaValue, ok := luaPayload.Data().(lua.LValue); ok {
					luaValues = append(luaValues, luaValue)
				}
			}
			if len(luaValues) > 0 {
				p.pubsub.Publish(m.Topic, luaValues...)
			}
			p.log.Debug("sent message to process", zap.Any("msg", m))
		}
		return nil
	}
}

func getLuaArgs(payloads payload.Payloads) []lua.LValue {
	args := make([]lua.LValue, 0, len(payloads))
	for _, p := range payloads {
		if lv, ok := p.Data().(lua.LValue); ok {
			args = append(args, lv)
		}
	}
	return args
}
