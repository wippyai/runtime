// SPDX-License-Identifier: MPL-2.0

package socket

import (
	"errors"
	"io"
	"strings"
	"sync"
)

const (
	// MaxResolveAddresses is the maximum number of IP addresses allowed in a resolve snapshot.
	MaxResolveAddresses = 64
	// MaxResolveAddressBytes is the maximum sum of bytes of all IP address strings in a resolve snapshot.
	MaxResolveAddressBytes = 4096
)

// ErrResolveLimit is returned when DNS resolution yields more addresses or total address bytes than permitted.
var ErrResolveLimit = errors.New("socket: resolve limit exceeded")

// DNSAddressProvider provides resolved DNS addresses and requires cleanup.
type DNSAddressProvider interface {
	io.Closer
	DNSAddresses() []string
}

// ResolvedAddresses holds a bounded snapshot of resolved DNS addresses.
// It implements io.Closer and DNSAddressProvider.
//
// Ownership lifecycle:
// ResolvedAddresses is constructed by the dispatcher worker upon successful host lookup.
// The dispatcher hands ownership to a PendingOperation.
// If the operation is canceled, timed out, or closed before consumption, PendingOperation
// closes ResolvedAddresses to release references.
// When consumed, the caller (such as a backend worker or runtime resource) takes exclusive
// ownership via PendingOperation.Take and is responsible for calling Close() when done.
//
// Concurrency protection:
// Internal access is synchronized via a mutex so concurrent Close() or DNSAddresses() calls
// during ownership transfer, cancellation, or teardown do not corrupt state or cause data races.
type ResolvedAddresses struct {
	addresses []string
	mu        sync.Mutex
	closed    bool
}

var (
	_ io.Closer          = (*ResolvedAddresses)(nil)
	_ DNSAddressProvider = (*ResolvedAddresses)(nil)
)

// NewResolvedAddresses validates limits and returns a new bounded snapshot of resolved addresses.
// Counts and total byte sum are verified BEFORE any allocations or string copies.
// If valid, strings are cloned into an owned slice to isolate the snapshot from caller modifications.
func NewResolvedAddresses(addresses []string) (*ResolvedAddresses, error) {
	if len(addresses) > MaxResolveAddresses {
		return nil, ErrResolveLimit
	}
	var totalBytes int
	for _, addr := range addresses {
		if len(addr) > MaxResolveAddressBytes-totalBytes {
			return nil, ErrResolveLimit
		}
		totalBytes += len(addr)
	}
	if addresses == nil {
		return &ResolvedAddresses{addresses: nil}, nil
	}
	cloned := make([]string, len(addresses))
	for i, addr := range addresses {
		cloned[i] = strings.Clone(addr)
	}
	return &ResolvedAddresses{
		addresses: cloned,
	}, nil
}

// DNSAddresses returns a copy of the resolved address strings, or nil if closed or empty.
func (r *ResolvedAddresses) DNSAddresses() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.addresses == nil {
		return nil
	}
	out := make([]string, len(r.addresses))
	copy(out, r.addresses)
	return out
}

// Close clears all address references and marks the snapshot as closed.
// It is idempotent and safe for concurrent calls.
func (r *ResolvedAddresses) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	r.addresses = nil
	return nil
}
