// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"context"
	"fmt"
	"sync"
	"time"

	raftapi "github.com/wippyai/runtime/api/cluster/raft"
)

type proposalResult struct {
	response *raftapi.ApplyResponse
	err      error
}

// proposalWaits bounds futures retained after callers cancel. Hashicorp futures
// have no cancellation operation: capacity belongs to the outstanding work and
// is released only when it actually finishes, never when its caller leaves.
type proposalWaits struct {
	slots           chan struct{}
	changed         chan struct{}
	mu              sync.Mutex
	bytes, maxBytes int
}

// Revalidate lifecycle after admission waits and before handing work to Raft.
// This is not rollback: once apply begins its outcome may remain unknown.
func proposalAdmissionError(ctx context.Context, stop <-chan struct{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-stop:
		return raftapi.ErrNotRunning
	default:
		return nil
	}
}

func (w *proposalWaits) reserveBytes(ctx context.Context, stop <-chan struct{}, size int) error {
	if w.maxBytes <= 0 || size > w.maxBytes {
		return fmt.Errorf("raft: proposal exceeds max_pending_apply_bytes")
	}
	for {
		if err := proposalAdmissionError(ctx, stop); err != nil {
			return err
		}
		w.mu.Lock()
		if err := proposalAdmissionError(ctx, stop); err != nil {
			w.mu.Unlock()
			return err
		}
		if size <= w.maxBytes-w.bytes {
			w.bytes += size
			w.mu.Unlock()
			return nil
		}
		if w.changed == nil {
			w.changed = make(chan struct{})
		}
		changed := w.changed
		w.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-stop:
			return raftapi.ErrNotRunning
		case <-changed:
		}
	}
}
func (w *proposalWaits) releaseBytes(size int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.bytes -= size
	if w.changed != nil {
		close(w.changed)
		w.changed = nil
	}
}

func (w *proposalWaits) run(ctx context.Context, stop <-chan struct{}, cmd []byte, apply func([]byte) (*raftapi.ApplyResponse, error)) (*raftapi.ApplyResponse, error) {
	if err := proposalAdmissionError(ctx, stop); err != nil {
		return nil, err
	}
	if w.maxBytes <= 0 || len(cmd) > w.maxBytes {
		return nil, fmt.Errorf("raft: proposal exceeds max_pending_apply_bytes")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-stop:
		return nil, raftapi.ErrNotRunning
	case w.slots <- struct{}{}:
	}
	if err := proposalAdmissionError(ctx, stop); err != nil {
		<-w.slots
		return nil, err
	}
	if err := w.reserveBytes(ctx, stop, len(cmd)); err != nil {
		<-w.slots
		return nil, err
	}
	if err := proposalAdmissionError(ctx, stop); err != nil {
		w.releaseBytes(len(cmd))
		<-w.slots
		return nil, err
	}
	owned := append([]byte(nil), cmd...)
	done := make(chan proposalResult, 1)
	go func() {
		defer func() { w.releaseBytes(len(owned)); <-w.slots }()
		if err := proposalAdmissionError(ctx, stop); err != nil {
			done <- proposalResult{err: err}
			return
		}
		response, err := apply(owned)
		done <- proposalResult{response: response, err: err}
	}()
	select {
	case out := <-done:
		return out.response, out.err
	case <-ctx.Done():
		select {
		case out := <-done:
			return out.response, out.err
		default:
		}
		return nil, fmt.Errorf("raft: proposal outcome unknown: %w", ctx.Err())
	case <-stop:
		select {
		case out := <-done:
			return out.response, out.err
		default:
		}
		return nil, fmt.Errorf("raft: proposal outcome unknown: %w", raftapi.ErrNotRunning)
	}
}

// ApplyContext lets the caller stop waiting for a proposal. Cancellation after
// submission does not roll it back and must not trigger an automatic retry.
// The timeout retains Apply's queue-admission meaning; ctx controls caller wait.
func (n *Node) ApplyContext(ctx context.Context, cmd []byte, timeout time.Duration) (*raftapi.ApplyResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	n.mu.Lock()
	running := n.raft != nil && n.started
	n.mu.Unlock()
	if !running {
		return nil, raftapi.ErrNotRunning
	}
	// The caller may reuse cmd when cancellation returns. Copy only after the
	// bounded admission gate is acquired.
	// Ownership must transfer before the waiting method can return.
	return n.proposals.run(ctx, n.stopCh, cmd, func(owned []byte) (*raftapi.ApplyResponse, error) {
		return n.Apply(owned, timeout)
	})
}

var _ raftapi.ContextApplier = (*Node)(nil)
