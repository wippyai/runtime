// SPDX-License-Identifier: MPL-2.0

package context

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/attrs"
)

// frameResolversCtx keys the FrameResolvers registry on the AppContext.
var frameResolversCtx = &Key{Name: "frame.resolvers"}

// FrameResolver maps a call's merged options to frame-context pairs applied to
// a newly spawned task or process frame. Resolvers are pure and stateless: they
// read ctx and options and emit pairs. This lets frame-decorating options (the
// network overlay, filesystem root, ...) be registered once at boot instead of
// hand-wired into every dispatcher.
type FrameResolver func(ctx context.Context, options attrs.Attributes) ([]Pair, error)

type frameResolverEntry struct {
	fn    FrameResolver
	name  string
	order int
}

// FrameResolvers is an ordered set of FrameResolver functions. Registration
// happens once at boot and rebuilds an immutable snapshot; Resolve reads that
// snapshot atomically with no lock, so the spawn path pays only an atomic load.
// A nil *FrameResolvers is a valid empty registry (Resolve is a no-op), so
// dispatchers never nil-check.
type FrameResolvers struct {
	snapshot atomic.Pointer[[]frameResolverEntry]
	mu       sync.Mutex // guards Register's copy-on-write
}

// NewFrameResolvers returns an empty registry.
func NewFrameResolvers() *FrameResolvers { return &FrameResolvers{} }

// Register adds a resolver under a unique name with an explicit apply order
// (ascending; ties broken by name). Returns an error on a nil function or a
// duplicate name. Intended to be called at boot only; it rebuilds the snapshot
// copy-on-write so Resolve never observes a partial update.
func (r *FrameResolvers) Register(name string, order int, fn FrameResolver) error {
	if fn == nil {
		return fmt.Errorf("frame resolver %q: nil function", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	var entries []frameResolverEntry
	if cur := r.snapshot.Load(); cur != nil {
		entries = make([]frameResolverEntry, len(*cur), len(*cur)+1)
		copy(entries, *cur)
	}
	for _, e := range entries {
		if e.name == name {
			return fmt.Errorf("frame resolver %q already registered", name)
		}
	}
	entries = append(entries, frameResolverEntry{fn: fn, name: name, order: order})
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].order != entries[j].order {
			return entries[i].order < entries[j].order
		}
		return entries[i].name < entries[j].name
	})
	r.snapshot.Store(&entries)
	return nil
}

// Resolve applies every registered resolver in order and appends the pairs each
// produces to pairs, returning the extended slice. It stops at the first
// resolver error, wrapping it with the resolver name (the cause is preserved
// for errors.Is). A nil receiver, or one with no resolvers, returns pairs
// unchanged. This is lock-free: it reads the current snapshot atomically.
func (r *FrameResolvers) Resolve(ctx context.Context, options attrs.Attributes, pairs []Pair) ([]Pair, error) {
	if r == nil {
		return pairs, nil
	}
	cur := r.snapshot.Load()
	if cur == nil {
		return pairs, nil
	}
	for _, e := range *cur {
		got, err := e.fn(ctx, options)
		if err != nil {
			return nil, fmt.Errorf("frame resolver %q: %w", e.name, err)
		}
		pairs = append(pairs, got...)
	}
	return pairs, nil
}

// WithFrameResolvers stores the registry on the AppContext (write-once, boot
// time). No-op when the AppContext is absent or already holds a registry.
func WithFrameResolvers(ctx context.Context, resolvers *FrameResolvers) context.Context {
	ac := AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(frameResolversCtx) == nil {
		ac.With(frameResolversCtx, resolvers)
	}
	return ctx
}

// FrameResolversFrom retrieves the registry from the AppContext, or nil when
// none is wired (Resolve on nil is a safe no-op).
func FrameResolversFrom(ctx context.Context) *FrameResolvers {
	ac := AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if v, ok := ac.Get(frameResolversCtx).(*FrameResolvers); ok {
		return v
	}
	return nil
}
