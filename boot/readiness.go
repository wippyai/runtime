package boot

import (
	"context"
	"sync"
	"sync/atomic"

	ctxapi "github.com/wippyai/runtime/api/context"
)

// Readiness coordinates boot-time readiness across services.
// Services may call Add/Done directly or use Track() for scoped lifecycle.
type Readiness struct {
	wg      sync.WaitGroup
	pending atomic.Int64
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
func (r *Readiness) Wait(ctx context.Context) error {
	if r == nil || r.Pending() == 0 {
		return nil
	}

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
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
