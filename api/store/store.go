// Package store provides generic storage abstractions.
package store

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

type (
	// Entry represents a key-value entry with optional TTL.
	Entry struct {
		Value payload.Payload
		Key   registry.ID
		TTL   time.Duration
	}

	// Store defines the interface for a key-value store.
	Store interface {
		// Get retrieves a value by key.
		Get(ctx context.Context, key registry.ID) (payload.Payload, error)

		// Set stores or updates a value with the given key.
		Set(ctx context.Context, entry Entry) error

		// Delete removes a value by key.
		Delete(ctx context.Context, key registry.ID) error

		// Has checks if a key exists without retrieving the value.
		Has(ctx context.Context, key registry.ID) (bool, error)
	}

	// ScanOptions configures a prefix scan operation.
	ScanOptions struct {
		Prefix string
		After  string
		Limit  int
	}

	// Scanner extends Store with prefix scan capability.
	Scanner interface {
		Store
		// Scan iterates over entries matching the prefix.
		// The callback returns true to continue, false to stop.
		Scan(ctx context.Context, opts ScanOptions, fn func(Entry) bool) error
	}

	// Version represents an entry version for optimistic concurrency.
	Version uint64

	// VersionedEntry adds version tracking to Entry.
	VersionedEntry struct {
		Entry
		Version Version // Version for CAS operations (0 = not found).
	}

	// Atomic extends Store with compare-and-swap capability.
	Atomic interface {
		Store
		// GetVersioned retrieves a value with its version.
		GetVersioned(ctx context.Context, key registry.ID) (VersionedEntry, error)

		// CompareAndSwap updates the entry only if version matches.
		// Returns true if swap succeeded, false if version mismatch.
		CompareAndSwap(ctx context.Context, key registry.ID, expected Version, entry Entry) (bool, error)

		// SetIfAbsent stores the entry only if the key does not exist.
		// Returns true if stored, false if key already exists.
		SetIfAbsent(ctx context.Context, entry Entry) (bool, error)
	}
)
