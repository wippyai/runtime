// SPDX-License-Identifier: MPL-2.0
package relay

import (
	"context"
	"sync"
	"sync/atomic"

	api "github.com/wippyai/runtime/api/relay"
)

// registrationLifetime fences native admission without holding a routing lock
// across receiver code. Retirement cancels calls and reserves the address until
// they return. The owner supplies exact compare-delete cleanup, never deletion
// by address alone. Initialization must precede publication.
type registrationLifetime struct {
	ctx     context.Context
	cancel  context.CancelFunc
	retired atomic.Bool
	mu      sync.Mutex
	active  int
	cleanup func()
}

func (r *registrationLifetime) init(cleanup func()) {
	r.ctx, r.cancel = context.WithCancel(context.Background())
	r.cleanup = cleanup
}

func (r *registrationLifetime) retire() { r.closeAdmission() }
func (r *registrationLifetime) closeAdmission() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.retired.Load() {
		return false
	}
	r.retired.Store(true)
	r.cancel()
	if r.active == 0 {
		r.cleanup()
	}
	return true
}

func (r *registrationLifetime) acquire() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.retired.Load() {
		return false
	}
	r.active++
	return true
}

func (r *registrationLifetime) releaseAdmission() {
	r.mu.Lock()
	r.active--
	if r.active == 0 && r.retired.Load() {
		r.cleanup()
	}
	r.mu.Unlock()
}

func (r *registrationLifetime) withContext(ctx context.Context, call func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !r.acquire() {
		return api.ErrBindingRetired
	}
	defer r.releaseAdmission()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.ctx, cancel)
	defer cancel()
	defer stop()
	if r.retired.Load() {
		return api.ErrBindingRetired
	}
	return call(ctx)
}
