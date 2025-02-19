package stub_process

import (
	"context"
	"errors"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/topology"
	"log"
	"sync"
	"time"
)

type TickerProcess struct {
	pid       pubsub.PID
	ticker    *time.Ticker
	count     int
	mu        sync.Mutex
	cancelled bool
	done      chan struct{}
	ctx       context.Context // capture context for later callback invocations
}

func NewTickerProcess() process.Process {
	return &TickerProcess{
		done: make(chan struct{}),
	}
}

func NewTickerPrototype() process.Prototype {
	return func() (process.Process, error) {
		return NewTickerProcess(), nil
	}
}

// Updated Start now uses the current API: (context, pid, payloads)
func (p *TickerProcess) Start(ctx context.Context, pid pubsub.PID, input payload.Payloads) error {
	p.ctx = ctx
	p.pid = pid
	p.ticker = time.NewTicker(time.Second)

	// Trigger onStart callback if present in the context.
	if onStart := process.GetOnStart(ctx); onStart != nil {
		onStart(p.pid, p)
	}

	// Keep ticker running even if context is done.
	go func() {
		select {
		case <-ctx.Done():
			log.Printf("--- ticker %v: context done but keeping ticker alive", p.pid)
		case <-p.done:
			return
		}
	}()

	log.Printf("--- ticker %v: started with input %v", p.pid, input)
	return nil
}

func (p *TickerProcess) Step() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	select {
	case <-p.done:
		return errors.New("unexpected step call")
	default:
	}

	if p.cancelled {
		close(p.done)
		if onComplete := process.GetOnComplete(p.ctx); onComplete != nil {
			onComplete(p.pid, &runtime.Result{Payload: payload.NewString("cancelled")})
		}
		return nil
	}

	if p.count == 5 {
		close(p.done)
		result := &runtime.Result{Error: errors.New("panic")}
		if onComplete := process.GetOnComplete(p.ctx); onComplete != nil {
			onComplete(p.pid, result)
		}
		return errors.New("panic")
	}

	select {
	case <-p.ticker.C:
		p.count++
		log.Printf("--- ticker %v: tick %d", p.pid, p.count)
	default:
	}

	return nil
}

func (p *TickerProcess) Send(msg *pubsub.Batch) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, m := range *msg {
		if m.Topic == process.TopicEvents {
			if len(m.Payloads) > 0 {
				cmd := m.Payloads[0].Data()
				if cmd.(events.Event).Kind == topology.KindCancel {
					p.cancelled = true
				}
			}
		}
	}

	return nil
}
