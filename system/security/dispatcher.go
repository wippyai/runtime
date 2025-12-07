// Package security provides token store command handlers for the dispatcher system.
package security

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	securityapi "github.com/wippyai/runtime/api/dispatcher/security"
	"github.com/wippyai/runtime/api/process"
)

// Dispatcher handles security commands via async worker pool.
type Dispatcher struct {
	workers int
	jobs    chan job
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

type job struct {
	ctx      context.Context
	cmd      dispatcher.Command
	tag      uint64
	receiver process.ResultReceiver
}

// NewDispatcher creates a security dispatcher with the specified worker count.
func NewDispatcher(workers int) *Dispatcher {
	if workers <= 0 {
		workers = 4
	}
	return &Dispatcher{workers: workers}
}

// Start initializes the worker pool.
func (d *Dispatcher) Start(ctx context.Context) error {
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.jobs = make(chan job, d.workers*2)

	for i := 0; i < d.workers; i++ {
		d.wg.Add(1)
		go d.worker()
	}
	return nil
}

// Stop shuts down the dispatcher and drains pending jobs.
func (d *Dispatcher) Stop(_ context.Context) error {
	d.cancel()
	close(d.jobs)
	d.wg.Wait()
	return nil
}

func (d *Dispatcher) worker() {
	defer d.wg.Done()
	for j := range d.jobs {
		d.execute(j)
	}
}

func (d *Dispatcher) submit(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver process.ResultReceiver) {
	select {
	case d.jobs <- job{ctx: ctx, cmd: cmd, tag: tag, receiver: receiver}:
	case <-d.ctx.Done():
	}
}

func (d *Dispatcher) execute(j job) {
	switch c := j.cmd.(type) {
	case *securityapi.TokenValidateCmd:
		actor, scope, err := c.TokenStore.Validate(j.ctx, c.Token)
		j.receiver.CompleteYield(j.tag, securityapi.TokenValidateResponse{Actor: actor, Scope: scope, Error: err}, nil)

	case *securityapi.TokenCreateCmd:
		token, err := c.TokenStore.Create(j.ctx, c.Actor, c.Scope, c.Details)
		j.receiver.CompleteYield(j.tag, securityapi.TokenCreateResponse{Token: token, Error: err}, nil)

	case *securityapi.TokenRevokeCmd:
		err := c.TokenStore.Revoke(j.ctx, c.Token)
		j.receiver.CompleteYield(j.tag, securityapi.TokenRevokeResponse{Error: err}, nil)
	}
}

func (d *Dispatcher) handle(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver process.ResultReceiver) error {
	d.submit(ctx, cmd, tag, receiver)
	return nil
}

// RegisterAll registers all security handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	h := dispatcher.HandlerFunc(d.handle)
	register(securityapi.CmdTokenValidate, h)
	register(securityapi.CmdTokenCreate, h)
	register(securityapi.CmdTokenRevoke, h)
}
