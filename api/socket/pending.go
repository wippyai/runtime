// SPDX-License-Identifier: MPL-2.0

package socket

import (
	"context"
	"io"
	"sync"
)

// PendingOperation is a one-shot future for a deferred socket start.
// The socket creates and owns the operation before dispatch.
type PendingOperation struct {
	ctx         context.Context
	cancel      context.CancelFunc
	value       io.Closer
	err         error
	notify      chan struct{}
	done        chan struct{}
	hookDone    chan struct{}
	stopHook    func() bool
	inflight    sync.WaitGroup
	mu          sync.Mutex
	closeOnce   sync.Once
	hookOnce    sync.Once
	started     bool
	finishing   bool
	completed   bool
	taken       bool
	closed      bool
	hookRunning bool
}

var _ io.Closer = (*PendingOperation)(nil)

// NewPendingOperation creates an idle future. Start runs at most once.
func NewPendingOperation() *PendingOperation {
	return &PendingOperation{
		notify: make(chan struct{}),
		done:   make(chan struct{}),
	}
}

// Start arms the network context at most once.
// Close before Start prevents a later Start and returns promptly.
func (p *PendingOperation) Start(ctx context.Context) (context.Context, bool) {
	if p == nil {
		return nil, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.started {
		return nil, false
	}
	p.started = true
	p.ctx, p.cancel = context.WithCancel(ctx)
	p.hookDone = make(chan struct{})
	p.stopHook = context.AfterFunc(ctx, p.onParentCancel)
	return p.ctx, true
}

// Complete records the worker result exactly once.
// A canceled or closed operation closes a late resource instead of storing it.
// An error with a resource closes the resource and keeps the error.
// Complete(nil, nil) records ErrNoResult.
// Rejected resources are closed before the operation becomes ready.
func (p *PendingOperation) Complete(value io.Closer, err error) {
	if p == nil {
		closeResult(value)
		return
	}

	p.mu.Lock()
	if p.completed || p.finishing {
		p.mu.Unlock()
		closeResult(value)
		return
	}
	p.finishing = true

	discard := value
	switch {
	case p.closed || contextErr(p.ctx) != nil:
		p.value = nil
		p.err = completeErr(p.ctx, err, p.closed)
	case err != nil:
		p.value = nil
		p.err = err
	case value == nil:
		p.value = nil
		p.err = ErrNoResult
		discard = nil
	default:
		p.value = value
		p.err = nil
		discard = nil
	}
	p.mu.Unlock()
	closeResult(discard)

	p.mu.Lock()
	p.completed = true
	p.finishLocked()
	p.mu.Unlock()
}

// Ready reports whether an owned completion is available to Take.
func (p *PendingOperation) Ready() bool {
	if p == nil {
		return false
	}
	select {
	case <-p.notify:
		return true
	default:
		return false
	}
}

// Notify returns the single readiness signal for this operation.
func (p *PendingOperation) Notify() <-chan struct{} {
	if p == nil {
		return nil
	}
	return p.notify
}

// Take transfers a completed result once.
// Pending returns nil, nil, false. A repeated Take returns a ready error.
// A successful result is not transferred after parent cancellation or Close.
//
//nolint:revive // Readiness-last matches the backend TCPNetworkOperation interface.
func (p *PendingOperation) Take() (io.Closer, error, bool) {
	if p == nil {
		return nil, nil, false
	}

	p.mu.Lock()
	if !p.completed {
		p.mu.Unlock()
		return nil, nil, false
	}
	if p.taken {
		p.mu.Unlock()
		return nil, ErrAlreadyTaken, true
	}
	p.taken = true
	value, err := p.value, p.err
	if value == nil || err != nil {
		p.mu.Unlock()
		return nil, err, true
	}
	if p.abandonedLocked() {
		if p.err == nil {
			p.err = p.abandonErrLocked()
		}
		err = p.err
		p.mu.Unlock()
		return nil, err, true
	}
	running := p.detachHookLocked()
	if running || p.abandonedLocked() {
		if p.err == nil {
			p.err = p.abandonErrLocked()
		}
		err = p.err
		var discard io.Closer
		if !running {
			discard = p.takeForCloseLocked()
		}
		p.mu.Unlock()
		p.finishClose(discard)
		return nil, err, true
	}
	p.value = nil
	cancel := p.cancel
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	p.joinHook(running)
	return value, nil, true
}

// Close cancels a started operation, joins the worker, and closes any leftover result.
func (p *PendingOperation) Close() error {
	if p == nil {
		return nil
	}
	p.closeOnce.Do(p.close)
	return nil
}

func (p *PendingOperation) close() {
	p.mu.Lock()
	p.closed = true
	started := p.started
	cancel := p.cancel
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if !started {
		p.mu.Lock()
		if !p.completed && !p.finishing {
			p.completed = true
			if p.err == nil {
				p.err = ErrOperationClosed
			}
			p.finishLocked()
		}
		leftover := p.takeForCloseLocked()
		p.mu.Unlock()
		p.finishClose(leftover)
		return
	}

	<-p.done

	p.mu.Lock()
	leftover := p.takeForCloseLocked()
	if leftover != nil && p.err == nil {
		p.err = ErrOperationClosed
	}
	running := p.detachHookLocked()
	p.mu.Unlock()

	p.joinHook(running)
	p.finishClose(leftover)
	p.inflight.Wait()
}

func (p *PendingOperation) onParentCancel() {
	defer p.finishHook()

	p.mu.Lock()
	p.hookRunning = true
	cancel := p.cancel
	value := p.takeForCloseLocked()
	if value != nil && p.err == nil {
		p.err = p.abandonErrLocked()
	}
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	p.finishClose(value)
}

func (p *PendingOperation) takeForCloseLocked() io.Closer {
	value := p.value
	p.value = nil
	if value != nil {
		p.inflight.Add(1)
	}
	return value
}

func (p *PendingOperation) finishClose(value io.Closer) {
	if value == nil {
		return
	}
	closeResult(value)
	p.inflight.Done()
}

func (p *PendingOperation) abandonedLocked() bool {
	return p.closed || contextErr(p.ctx) != nil
}

func (p *PendingOperation) abandonErrLocked() error {
	if err := contextErr(p.ctx); err != nil {
		return err
	}
	if p.closed {
		return ErrOperationClosed
	}
	return context.Canceled
}

func (p *PendingOperation) detachHookLocked() bool {
	if p.stopHook != nil {
		if !p.stopHook() {
			p.hookRunning = true
		}
		p.stopHook = nil
	}
	return p.hookRunning
}

func (p *PendingOperation) joinHook(running bool) {
	if running && p.hookDone != nil {
		<-p.hookDone
	}
}

func (p *PendingOperation) finishHook() {
	p.hookOnce.Do(func() {
		if p.hookDone != nil {
			close(p.hookDone)
		}
	})
}

func (p *PendingOperation) finishLocked() {
	close(p.notify)
	close(p.done)
}

func completeErr(ctx context.Context, err error, closed bool) error {
	if err != nil {
		return err
	}
	if e := contextErr(ctx); e != nil {
		return e
	}
	if closed {
		return ErrOperationClosed
	}
	return context.Canceled
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func closeResult(value io.Closer) {
	if value != nil {
		_ = value.Close()
	}
}
