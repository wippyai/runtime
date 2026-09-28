// SPDX-License-Identifier: MPL-2.0

package context

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/attrs"
)

// frameResolversCtx keys the FrameResolvers registry on the AppContext.
var frameResolversCtx = &Key{Name: "frame.resolvers"}

// ErrFrameResolverNotRegistered is returned when an options bag contains a key
// claimed by a frame-context resolver, but no registered resolver covers it.
var ErrFrameResolverNotRegistered = errors.New("frame resolver not registered")

var (
	frameClaimsMu sync.Mutex
	frameClaims   atomic.Pointer[map[string]FrameResolverClaim]
)

// FrameResolver maps a call's merged options to frame-context pairs applied to
// a newly spawned task or process frame. Resolvers are pure and stateless: they
// read ctx and options and emit pairs. This lets frame-decorating options (the
// network overlay, filesystem root, ...) be registered once at boot instead of
// hand-wired into every dispatcher.
type FrameResolver func(ctx context.Context, options attrs.Attributes) ([]Pair, error)

// FrameResolverClaim reports whether a frame-context selection is active for a
// call. It lets Resolve fail closed when a subsystem option such as the network
// overlay was selected but the corresponding resolver was not boot-registered.
type FrameResolverClaim func(ctx context.Context, options attrs.Attributes) bool

type frameResolverEntry struct {
	fn     FrameResolver
	name   string
	claims []string
	order  int
}

// FrameResolvers is an ordered set of FrameResolver functions. Registration
// happens once at boot and rebuilds an immutable snapshot; Resolve reads that
// snapshot atomically with no lock, so the spawn path pays only an atomic load.
// A nil *FrameResolvers is a valid empty registry for ordinary options, but
// Resolve still fails closed when the options bag contains a globally claimed
// frame option that no registered resolver covers.
type FrameResolvers struct {
	snapshot      atomic.Pointer[[]frameResolverEntry]
	claimSnapshot atomic.Pointer[map[string]FrameResolverClaim]
	mu            sync.Mutex // guards registration copy-on-write
}

// NewFrameResolvers returns an empty registry.
func NewFrameResolvers() *FrameResolvers { return &FrameResolvers{} }

// RegisterClaim reserves a frame option on this resolver registry. Claims are
// installed by the boot composition root so subsystem packages do not need
// import-time registration. A selected claim without a covering resolver makes
// Resolve fail closed.
func (r *FrameResolvers) RegisterClaim(name string, selected FrameResolverClaim) error {
	if name == "" {
		return errors.New("frame resolver claim name cannot be empty")
	}
	if selected == nil {
		return fmt.Errorf("frame resolver claim %q: nil function", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	next := map[string]FrameResolverClaim{name: selected}
	if current := r.claimSnapshot.Load(); current != nil {
		next = make(map[string]FrameResolverClaim, len(*current)+1)
		for key, claim := range *current {
			if key == name {
				return fmt.Errorf("frame resolver claim %q already registered", name)
			}
			next[key] = claim
		}
		next[name] = selected
	}
	r.claimSnapshot.Store(&next)
	return nil
}

// RegisterFrameResolverClaim claims a frame-context selection. If selected
// returns true during dispatch but no resolver registered for this name, Resolve
// returns ErrFrameResolverNotRegistered instead of silently ignoring it.
//
// Packages that define frame-context selections should call this from init,
// then pass the same claim name to FrameResolvers.Register when their boot
// component wires the resolver. Duplicate claim names panic: claims are
// process-global, and silently replacing one could make missing-resolver
// validation fail open.
func RegisterFrameResolverClaim(name string, selected FrameResolverClaim) {
	if name == "" {
		panic("frame resolver claim name cannot be empty")
	}
	if selected == nil {
		panic("frame resolver claim cannot be nil")
	}
	frameClaimsMu.Lock()
	defer frameClaimsMu.Unlock()

	next := map[string]FrameResolverClaim{name: selected}
	if cur := frameClaims.Load(); cur != nil {
		next = make(map[string]FrameResolverClaim, len(*cur)+1)
		for k, v := range *cur {
			if k == name {
				panic(fmt.Sprintf("frame resolver claim %q already registered", name))
			}
			next[k] = v
		}
		next[name] = selected
	}
	frameClaims.Store(&next)
}

// Register adds a resolver under a unique name with an explicit apply order
// (ascending; ties broken by name). Returns an error on a nil function or a
// duplicate name. claims names the claimed frame selections this resolver
// covers; Resolve fails closed if such a selection is active but the resolver
// was not registered. Intended to be called at boot only; it rebuilds the snapshot
// copy-on-write so Resolve never observes a partial update.
func (r *FrameResolvers) Register(name string, order int, fn FrameResolver, claims ...string) error {
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
	entries = append(entries, frameResolverEntry{
		fn:     fn,
		name:   name,
		order:  order,
		claims: append([]string(nil), claims...),
	})
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
// unchanged unless a claimed frame selection is active and no resolver covers
// it. This is lock-free and allocation-free: it reads immutable snapshots
// atomically.
func (r *FrameResolvers) Resolve(ctx context.Context, options attrs.Attributes, pairs []Pair) ([]Pair, error) {
	if r == nil {
		return pairs, validateFrameResolverClaims(ctx, options, nil, nil)
	}
	cur := r.snapshot.Load()
	if cur == nil {
		return pairs, validateFrameResolverClaims(ctx, options, nil, r.claimSnapshot.Load())
	}
	if err := validateFrameResolverClaims(ctx, options, *cur, r.claimSnapshot.Load()); err != nil {
		return nil, err
	}
	base := len(pairs)
	for _, e := range *cur {
		got, err := e.fn(ctx, options)
		if err != nil {
			rollbackErr := errors.Join(
				rollbackFramePairs(got),
				rollbackFramePairs(pairs[base:]),
			)
			return nil, fmt.Errorf("frame resolver %q: %w", e.name, errors.Join(err, rollbackErr))
		}
		pairs = append(pairs, got...)
	}
	return pairs, nil
}

func validateFrameResolverClaims(ctx context.Context, options attrs.Attributes, entries []frameResolverEntry, local *map[string]FrameResolverClaim) error {
	if err := validateClaimSet(ctx, options, entries, local); err != nil {
		return err
	}
	return validateClaimSet(ctx, options, entries, frameClaims.Load())
}

func validateClaimSet(ctx context.Context, options attrs.Attributes, entries []frameResolverEntry, claims *map[string]FrameResolverClaim) error {
	if claims == nil {
		return nil
	}
	for name, selected := range *claims {
		if frameResolverClaimCovered(entries, name) {
			continue
		}
		if !selected(ctx, options) {
			continue
		}
		return fmt.Errorf("%w for %q", ErrFrameResolverNotRegistered, name)
	}
	return nil
}

func rollbackFramePairs(pairs []Pair) error {
	var result error
	for index := len(pairs) - 1; index >= 0; index-- {
		if attachment, ok := pairs[index].Value.(FrameAttachment); ok {
			result = errors.Join(result, attachment.Rollback())
		}
	}
	return result
}

func frameResolverClaimCovered(entries []frameResolverEntry, name string) bool {
	for _, e := range entries {
		if e.name == name {
			return true
		}
		for _, claim := range e.claims {
			if claim == name {
				return true
			}
		}
	}
	return false
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
// none is wired. Calling Resolve on nil is allowed, but still rejects selected
// frame resolver claims that were globally registered and are not covered by a
// registered resolver.
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
