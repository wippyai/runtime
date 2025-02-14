package btea

import (
	"context"
	"errors"
	"sync"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"go.uber.org/zap"
)

// Process implements process.Process with runner management
type Process struct {
	mu     sync.RWMutex
	log    *zap.Logger
	dtt    payload.Transcoder
	runner *engine.Runner
	inner  process.Process // todO: replace it
}

// NewBteaProcess constructs a new Process instance
func NewBteaProcess(
	log *zap.Logger,
	dtt payload.Transcoder,
	runner *engine.Runner,
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

	return &Process{
		log:    log,
		dtt:    dtt,
		runner: runner,
		inner:  NewTerminalProcess(),
	}, nil
}

// Start initializes the process
func (p *Process) Start(ctx context.Context, pid process.PID, input payload.Payloads) error {
	return p.inner.Start(ctx, pid, input)
}

// Step updates the process state
func (p *Process) Step() error {
	p.mu.RLock()
	runner := p.runner
	p.mu.RUnlock()

	if runner != nil {
		// TODO: Implement runner step logic when needed
		// This is where we'll handle runner execution
	}

	return p.inner.Step()
}

// Send routes messages to the process
func (p *Process) Send(msg ...*process.Message) error {
	p.mu.RLock()
	runner := p.runner
	p.mu.RUnlock()

	if runner != nil {
		// TODO: Implement message handling with runner when needed
	}

	return p.inner.Send(msg...)
}

// Close cleans up resources
func (p *Process) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.runner != nil {
		p.runner.Close()
		p.runner = nil
	}

	return nil
}
