// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"time"
)

// Mode picks the consistency tier for a KV space. Different modes ride
// different cluster substrates: ModeRaft uses Raft consensus,
// ModeEventual uses gossip-replicated CRDTs.
type Mode uint8

const (
	// ModeRaft is strongly-consistent, linearizable, supports CAS. Sized
	// for ~10k keys (configmap-class).
	ModeRaft Mode = 1
	// ModeEventual is eventually-consistent via gossip CRDT, no global
	// coordination, no CAS. Sized for ~1M keys (cache/session-class).
	ModeEventual Mode = 2
)

// Op identifies a Watch event kind.
type Op uint8

const (
	// OpPut indicates a Put or successful CompareAndSwap.
	OpPut Op = 1
	// OpDelete indicates a Delete or expiration via TTL reaper.
	OpDelete Op = 2
)

// Value is the result of Get / Scan / Watch. The interpretation of Version
// depends on Mode:
//   - ModeRaft: monotonic Raft log index for the apply that produced this
//     value. Stable cluster-wide.
//   - ModeEventual: per-origin CRDT counter encoded as a single uint64
//     (high 16 bits = origin compact id, low 48 bits = counter). Use only
//     for change-detection within a single replica's view.
type Value struct {
	TTL     time.Time // zero == no TTL
	Data    []byte
	Version uint64
}

// Event is delivered on a Watch channel for each successful change to a key
// matching the watched prefix.
type Event struct {
	Key   string
	Value Value
	Op    Op
}

// PutOption configures a Put call. Options are zero-cost when unused —
// putOpts has zero defaults that mean "no constraint".
type PutOption interface {
	apply(*putOpts)
}

type putOpts struct {
	ttl              time.Duration
	expectVersion    uint64
	hasExpectVersion bool
	expectAbsent     bool
}

type putOptionFn func(*putOpts)

func (f putOptionFn) apply(o *putOpts) { f(o) }

// WithTTL attaches a time-to-live to a Put. The key is reaped after the
// TTL elapses. Zero or negative duration means "no TTL".
func WithTTL(d time.Duration) PutOption {
	return putOptionFn(func(o *putOpts) {
		if d > 0 {
			o.ttl = d
		}
	})
}

// WithExpectVersion turns Put into a CAS-by-version: the Put succeeds only
// if the current value's Version matches the expected one. Returns
// ErrCASMismatch otherwise. Supported by ModeRaft only — ModeEventual
// returns ErrUnsupported.
func WithExpectVersion(v uint64) PutOption {
	return putOptionFn(func(o *putOpts) {
		o.expectVersion = v
		o.hasExpectVersion = true
	})
}

// WithExpectAbsent makes Put fail with ErrKeyExists if the key is currently
// held. Equivalent to SetIfAbsent. Supported by both modes; in ModeEventual
// the check is best-effort against the local CRDT replica.
func WithExpectAbsent() PutOption {
	return putOptionFn(func(o *putOpts) {
		o.expectAbsent = true
	})
}

// PutOpts (exported for backend implementations): Backends use this struct
// to read the option set after applying them. Not part of the public API
// surface — it's lowercase-fielded but exported as a struct so backends in
// other packages can access it via the helpers below.
type PutOpts struct {
	TTL              time.Duration
	ExpectVersion    uint64
	HasExpectVersion bool
	ExpectAbsent     bool
}

// CollectPutOptions applies all options and returns the resolved set.
// Backend implementations call this exactly once per Put.
func CollectPutOptions(opts []PutOption) PutOpts {
	var o putOpts
	for _, opt := range opts {
		opt.apply(&o)
	}
	return PutOpts{
		TTL:              o.ttl,
		ExpectVersion:    o.expectVersion,
		HasExpectVersion: o.hasExpectVersion,
		ExpectAbsent:     o.expectAbsent,
	}
}

// KV is the contract callers consume for a single space. Spaces are namespace
// boundaries — keys in `config` and `sessions` cannot collide.
type KV interface {
	// Mode returns the consistency tier this space rides.
	Mode() Mode

	// Name returns the space name (the namespace key).
	Name() string

	// Get returns the live Value for `key`, or ErrKeyNotFound if absent.
	Get(ctx context.Context, key string) (Value, error)

	// Put writes `data` under `key`. WithTTL, WithExpectVersion, and
	// WithExpectAbsent modify the semantics — see each option.
	Put(ctx context.Context, key string, data []byte, opts ...PutOption) error

	// Delete removes `key`. Idempotent: deleting an absent key returns nil.
	Delete(ctx context.Context, key string) error

	// CompareAndSwap atomically replaces `expected` with `newVal`. Returns
	// ErrCASMismatch if the current value differs from `expected`.
	// ModeEventual returns ErrUnsupported.
	CompareAndSwap(ctx context.Context, key string, expected, newVal []byte) error

	// Watch streams Events for keys matching `prefix`. Cancel via ctx.
	// Backpressure: per-watcher buffered channel; on overflow the oldest
	// event is dropped and a kv_watch_dropped_total counter is incremented.
	// Caller MUST drain promptly.
	Watch(ctx context.Context, prefix string) (<-chan Event, error)

	// Scan walks keys in [start, end). `end == ""` means "to the end".
	// `fn` returns false to stop. Snapshot semantics: a single pass over
	// the local replica at the time of call.
	Scan(ctx context.Context, start, end string, fn func(key string, v Value) bool) error

	// Close releases the space. Subsequent ops return ErrSpaceClosed.
	// Implementations must be safe to call concurrently with in-flight ops.
	Close() error
}

// ProviderRegistry yields KV spaces by name + mode. Boot wires concrete
// backend factories into a default registry stored in context (see
// context.go).
type ProviderRegistry interface {
	// OpenRaft returns the KV space backed by Raft for `name`. Multiple
	// callers receive the same handle (spaces are reused).
	OpenRaft(name string) (KV, error)

	// OpenEventual returns the KV space backed by gossip CRDT for `name`.
	OpenEventual(name string) (KV, error)
}
