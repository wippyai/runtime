package stub_process

import (
	"context"
	"errors"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/runtime"
	"log"
	"sync"
	"time"
)

type TickerProcess struct {
	pid        process.PID
	onComplete []process.OnComplete
	ticker     *time.Ticker
	count      int
	mu         sync.Mutex
	cancelled  bool
	done       chan struct{}
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

func (p *TickerProcess) Start(ctx context.Context, start process.StartProcess) error {
	p.pid = start.PID
	p.onComplete = start.OnComplete
	p.ticker = time.NewTicker(time.Second)

	// Keep ticker running even if context is done
	go func() {
		select {
		case <-ctx.Done():
			log.Printf("ticker %v: context done but keeping ticker alive", p.pid)
		case <-p.done:
			return
		}
	}()

	log.Printf("ticker %v: started with input %v", start.PID, start.Input)
	return nil
}

func (p *TickerProcess) Step() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancelled {
		close(p.done)
		for _, handler := range p.onComplete {
			handler(p.pid, &runtime.Result{Payload: payload.NewString("cancelled")})
		}
		return nil
	}

	if p.count == 5 {
		result := &runtime.Result{Error: errors.New("panic")}
		for _, handler := range p.onComplete {
			handler(p.pid, result)
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

func (p *TickerProcess) Send(msg *process.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	log.Printf("--- ticker %v: received message topic=%s payload=%v", p.pid, msg.Topic, msg.Payload)

	if msg.Topic == process.TopicCancel {
		p.cancelled = true
	}

	return nil
}
