// Package store provides generic storage abstractions.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

// Common errors that can be returned by Store implementations
var (
	// ErrKeyNotFound indicates the requested key doesn't exist in the store
	// Returned by Get and Delete operations when the key cannot be found
	ErrKeyNotFound = errors.New("key not found")

	// ErrKeyExists indicates attempted creation of a key that already exists
	// Used when trying to create a new entry with a key that's already in use
	ErrKeyExists = errors.New("key already exists")

	// ErrInvalidKey indicates the key format is invalid
	// Returned when a key doesn't conform to the expected format or contains invalid characters
	ErrInvalidKey = errors.New("invalid key format")

	// ErrStoreFull indicates the store has reached its maximum capacity
	// Returned by Set operations when the store cannot accept more entries due to size constraints
	ErrStoreFull = errors.New("store is full, cannot add more entries")

	// ErrStoreClosed indicates the store is not open for operations
	// Returned when attempting to perform operations on a store that has been shut down
	ErrStoreClosed = errors.New("store is closed for operations, cannot perform action")
)

type (
	// Entry represents a complete key-value entry to be stored
	// Contains all the information needed to store a value in the key-value store
	Entry struct {
		// Key is the unique identifier for this entry
		Key registry.ID

		// Value is the data payload associated with this key
		Value payload.Payload

		// TTL (Time To Live) is the duration after which this entry should expire
		// Zero value (0) means the entry never expires
		TTL time.Duration
	}

	// Store defines the primary interface for a key-value store
	// All KV store implementations must satisfy this interface
	Store interface {
		// Get retrieves a value by key
		// Returns the payload associated with the given registry.ID or ErrKeyNotFound if not present
		// Other possible errors include ErrStoreClosed or implementation-specific errors
		Get(ctx context.Context, key registry.ID) (payload.Payload, error)

		// Set stores or updates a value with the given key
		// Overwrites any existing value if the key already exists
		// Returns ErrStoreFull if the store has reached capacity and cannot accept more entries
		// May also return ErrStoreClosed or implementation-specific errors
		Set(ctx context.Context, entry Entry) error

		// Delete removes a value with the given key
		// Returns ErrKeyNotFound if the key doesn't exist
		// May also return ErrStoreClosed or implementation-specific errors
		Delete(ctx context.Context, key registry.ID) error

		// Has checks if a key exists without retrieving the value
		// More efficient than Get when only checking for existence
		// Returns true if the key exists, false otherwise
		// May return errors like ErrStoreClosed or implementation-specific errors
		Has(ctx context.Context, key registry.ID) (bool, error)
	}

	// todo: prefix operations?
)
