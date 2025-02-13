package stub_process

import (
	"context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/runtime"
	"log"
	"sync"
	"time"
)

type TickerProcess struct {
	pid        process.PID
	onComplete func(process.PID, runtime.Result)
	ticker     *time.Ticker
	count      int
	mu         sync.Mutex
	terminated bool
}

func NewTickerProcess() process.Process {
	return &TickerProcess{}
}

func NewTickerPrototype() process.Prototype {
	return func() (process.Process, error) {
		return NewTickerProcess(), nil
	}
}

func (p *TickerProcess) Start(ctx context.Context, pid process.PID, input payload.Payloads) error {
	p.pid = pid
	p.ticker = time.NewTicker(time.Second)
	log.Printf("ticker %v: started with input %v", pid, input)
	return nil
}

func (p *TickerProcess) Step() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.terminated {
		if p.onComplete != nil {
			p.onComplete(p.pid, runtime.Result{})
		}
		return nil
	}

	select {
	case <-p.ticker.C:
		p.count++
		log.Printf("ticker %v: tick %d", p.pid, p.count)
	default:
	}
	return nil
}

func (p *TickerProcess) Send(msg process.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	log.Printf("ticker %v: received message topic=%s payload=%v", p.pid, msg.Topic, msg.Payload)

	if msg.Topic == "terminate" {
		p.terminated = true
		p.ticker.Stop()
	}
	return nil
}

func (p *TickerProcess) OnComplete(fn func(process.PID, runtime.Result)) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.terminated {
		return false
	}
	p.onComplete = fn
	return true
}
