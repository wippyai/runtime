// Package security provides token store command handlers for the dispatcher system.
// Supports both blocking (for testing) and async (for production) execution modes.
package security

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	securityapi "github.com/wippyai/runtime/api/dispatcher/security"
)

// job represents a unit of work for the async dispatcher.
type job struct {
	ctx  context.Context
	cmd  dispatcher.Command
	emit dispatcher.EmitFunc
}

// Dispatcher handles security commands with configurable execution mode.
type Dispatcher struct {
	workers int
	jobs    chan job
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

// Config holds dispatcher configuration.
type Config struct {
	// Workers is the number of worker goroutines for async mode.
	// If 0, dispatcher runs in blocking mode (synchronous execution).
	Workers int
}

// NewDispatcher creates a new security dispatcher with the given configuration.
func NewDispatcher(cfg Config) *Dispatcher {
	return &Dispatcher{
		workers: cfg.Workers,
	}
}

// NewBlockingDispatcher creates a dispatcher that executes synchronously.
func NewBlockingDispatcher() *Dispatcher {
	return &Dispatcher{workers: 0}
}

// NewAsyncDispatcher creates a dispatcher with a worker pool.
func NewAsyncDispatcher(workers int) *Dispatcher {
	if workers <= 0 {
		workers = 4
	}
	return &Dispatcher{workers: workers}
}

// Start initializes the dispatcher. For async mode, starts worker goroutines.
func (d *Dispatcher) Start(ctx context.Context) error {
	if d.workers <= 0 {
		return nil
	}

	d.ctx, d.cancel = context.WithCancel(ctx)
	d.jobs = make(chan job, d.workers*2)

	for i := 0; i < d.workers; i++ {
		d.wg.Add(1)
		go d.worker()
	}

	return nil
}

// Stop shuts down the dispatcher and waits for workers to finish.
func (d *Dispatcher) Stop(_ context.Context) error {
	if d.workers <= 0 {
		return nil
	}

	d.cancel()
	close(d.jobs)
	d.wg.Wait()
	return nil
}

// worker processes jobs from the queue.
func (d *Dispatcher) worker() {
	defer d.wg.Done()

	for j := range d.jobs {
		execute(j.ctx, j.cmd, j.emit)
	}
}

// submit sends a job to the worker pool.
func (d *Dispatcher) submit(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) {
	select {
	case d.jobs <- job{ctx: ctx, cmd: cmd, emit: emit}:
	case <-d.ctx.Done():
	}
}

// isAsync returns true if dispatcher is in async mode.
func (d *Dispatcher) isAsync() bool {
	return d.workers > 0 && d.jobs != nil
}

// execute runs the security operation and emits the result.
func execute(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) {
	switch c := cmd.(type) {
	case *securityapi.TokenValidateCmd:
		actor, scope, err := c.TokenStore.Validate(ctx, c.Token)
		emit(securityapi.TokenValidateResponse{
			Actor: actor,
			Scope: scope,
			Error: err,
		})

	case *securityapi.TokenCreateCmd:
		token, err := c.TokenStore.Create(ctx, c.Actor, c.Scope, c.Details)
		emit(securityapi.TokenCreateResponse{
			Token: token,
			Error: err,
		})

	case *securityapi.TokenRevokeCmd:
		err := c.TokenStore.Revoke(ctx, c.Token)
		emit(securityapi.TokenRevokeResponse{
			Error: err,
		})
	}
}

// ValidateHandler handles token validate commands.
type ValidateHandler struct {
	d *Dispatcher
}

func (h *ValidateHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	execute(ctx, cmd, emit)
	return nil
}

// CreateHandler handles token create commands.
type CreateHandler struct {
	d *Dispatcher
}

func (h *CreateHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	execute(ctx, cmd, emit)
	return nil
}

// RevokeHandler handles token revoke commands.
type RevokeHandler struct {
	d *Dispatcher
}

func (h *RevokeHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	execute(ctx, cmd, emit)
	return nil
}

// RegisterAll registers all security handlers with the given registry function.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(securityapi.CmdTokenValidate, &ValidateHandler{d: d})
	register(securityapi.CmdTokenCreate, &CreateHandler{d: d})
	register(securityapi.CmdTokenRevoke, &RevokeHandler{d: d})
}

// Service is an alias for Dispatcher for backward compatibility.
type Service = Dispatcher

// NewService creates a blocking dispatcher for backward compatibility.
func NewService() *Dispatcher {
	return NewBlockingDispatcher()
}
