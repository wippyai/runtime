// SPDX-License-Identifier: MPL-2.0

// Package kv provides cluster-level key-value contracts. Implementations
// land in `runtime/system/kvraft` (strongly-consistent via Raft) and
// `runtime/system/kveventual` (eventually-consistent via gossip CRDT).
package kv

import "errors"

// Sentinel errors. Implementations wrap these via fmt.Errorf("...: %w", ...)
// so callers can errors.Is against them.
var (
	// ErrKeyNotFound is returned by Get when no live entry exists for the key.
	ErrKeyNotFound = errors.New("kv: key not found")

	// ErrKeyExists is returned by SetIfAbsent / Put with WithExpectAbsent
	// when the key is already held.
	ErrKeyExists = errors.New("kv: key exists")

	// ErrCASMismatch is returned by CompareAndSwap when the expected value
	// does not match the current value, or by Put with WithExpectVersion
	// when versions diverge.
	ErrCASMismatch = errors.New("kv: CAS mismatch")

	// ErrUnsupported is returned for operations not supported by a backend.
	// Notably ModeEventual returns this for CompareAndSwap.
	ErrUnsupported = errors.New("kv: operation not supported by mode")

	// ErrSpaceClosed is returned by any op after the space has been closed.
	ErrSpaceClosed = errors.New("kv: space closed")

	// ErrWatchCanceled is delivered on the watch channel close path when
	// the watcher's context is canceled or the space is shutting down.
	ErrWatchCanceled = errors.New("kv: watch canceled")

	// ErrSpaceUnknown is returned when the requested space name has not been
	// registered with the provider.
	ErrSpaceUnknown = errors.New("kv: space unknown")
)
