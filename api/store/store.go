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
		Key   registry.ID     // Unique identifier for this entry.
		Value payload.Payload // Data payload associated with this key.
		TTL   time.Duration   // Time to live; zero means no expiration.
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
)
