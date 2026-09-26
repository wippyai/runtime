// SPDX-License-Identifier: MPL-2.0

package boot

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	ctxapi "github.com/wippyai/runtime/api/context"
)

// GateError is the typed error returned when a boot readiness gate fails.
type GateError struct {
	Err     error
	Service string
}

func (e *GateError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("boot gate %q failed: %v", e.Service, e.Err)
	}
	return fmt.Sprintf("boot gate %q failed", e.Service)
}

func (e *GateError) Unwrap() error {
	return e.Err
}

// GateState represents the lifecycle state of a boot gate.
type GateState int

const (
	// GateStatePending indicates the gate is registered and waiting for completion.
	GateStatePending GateState = iota
	// GateStateReady indicates the service completed successfully and the gate passed.
	GateStateReady
	// GateStateFailed indicates the service failed, crashed, or was stopped.
	GateStateFailed
)

// Gate represents a boot readiness gate declared by a service.
type Gate struct {
	readiness *Readiness
	id        string
	state     GateState
	mu        sync.Mutex
}

// Ready marks the gate as completed successfully.
// Once marked Ready or Failed, subsequent calls are no-ops.
func (g *Gate) Ready() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != GateStatePending {
		return
	}
	g.state = GateStateReady
	if g.readiness != nil {
		g.readiness.Done()
	}
}

// Fail marks the gate as failed with the given error.
// Once marked Ready or Failed, subsequent calls are no-ops.
func (g *Gate) Fail(err error) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != GateStatePending {
		return
	}
	g.state = GateStateFailed
	if g.readiness != nil {
		g.readiness.Fail(&GateError{
			Service: g.id,
			Err:     err,
		})
	}
}

// State returns the current gate state.
func (g *Gate) State() GateState {
	if g == nil {
		return GateStateReady
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state
}

// Readiness coordinates boot-time readiness across services.
// Services may call Add/Done directly or use Track() for scoped lifecycle.
type Readiness struct {
	err     error
	wg      sync.WaitGroup
	pending atomic.Int64
	mu      sync.Mutex
}

// NewReadiness creates a new readiness coordinator.
func NewReadiness() *Readiness {
	return &Readiness{}
}

// Add increments the number of pending readiness tasks.
func (r *Readiness) Add(delta int) {
	if r == nil || delta <= 0 {
		return
	}
	r.pending.Add(int64(delta))
	r.wg.Add(delta)
}

// Done marks a readiness task as completed.
func (r *Readiness) Done() {
	if r == nil {
		return
	}

	for {
		current := r.pending.Load()
		if current <= 0 {
			return
		}
		if r.pending.CompareAndSwap(current, current-1) {
			r.wg.Done()
			return
		}
	}
}

// Fail records a failure error and marks one readiness task as completed.
func (r *Readiness) Fail(err error) {
	if r == nil {
		return
	}
	if err != nil {
		r.mu.Lock()
		if r.err == nil {
			r.err = err
		} else {
			r.err = errors.Join(r.err, err)
		}
		r.mu.Unlock()
	}
	r.Done()
}

// RegisterGate registers a boot gate for the given service ID and returns a Gate handle.
func (r *Readiness) RegisterGate(id string) *Gate {
	gate := &Gate{
		readiness: r,
		id:        id,
		state:     GateStatePending,
	}
	if r != nil {
		r.Add(1)
	}
	return gate
}

// Track registers one readiness task and returns a completion function.
func (r *Readiness) Track() func() {
	r.Add(1)
	return func() { r.Done() }
}

// Pending returns the number of outstanding readiness tasks.
func (r *Readiness) Pending() int64 {
	if r == nil {
		return 0
	}
	return r.pending.Load()
}

// Wait blocks until all readiness tasks are completed or context is canceled.
// Returns any failure error recorded during readiness.
func (r *Readiness) Wait(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if r.Pending() == 0 {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.err
	}

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

var readinessKey = &ctxapi.Key{Name: "boot.readiness"}

// WithReadiness stores the readiness coordinator in AppContext.
func WithReadiness(ctx context.Context, readiness *Readiness) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(readinessKey) == nil {
		ac.With(readinessKey, readiness)
	}
	return ctx
}

// GetReadiness retrieves the readiness coordinator from AppContext.
func GetReadiness(ctx context.Context) *Readiness {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(readinessKey); val != nil {
		if readiness, ok := val.(*Readiness); ok {
			return readiness
		}
	}
	return nil
}
