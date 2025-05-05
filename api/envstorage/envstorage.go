package envstorage

import (
	"context"
	"errors"
)

// Manager defines the interface for environment variable storage
type Manager interface {
	// Start initializes the storage
	Start(ctx context.Context) error
	// Stop cleans up storage resources
	Stop() error
}

// Storage defines the interface for environment variable storage implementations
type Storage interface {
	// Get retrieves the value of an environment variable
	Get(ctx context.Context, key string) (string, bool)
	// Set sets the value of an environment variable
	Set(ctx context.Context, key, value string) error
	// Delete removes an environment variable
	Delete(ctx context.Context, key string) error
}

// StorageType defines the type of storage implementation
type StorageType string

const (
	// TypeMemory represents an in-memory storage implementation
	TypeMemory StorageType = "memory"
	// TypeFile represents a file-based storage implementation
	TypeFile StorageType = "file"
	// TypeDatabase represents a database-backed storage implementation
	TypeDatabase StorageType = "database"
)

// StorageConfig holds configuration for storage implementations
type StorageConfig struct {
	// Type specifies the storage implementation type
	Type StorageType
	// Path is the location for file/database storage (if applicable)
	Path string
	// Options contains additional implementation-specific options
	Options map[string]string
}

// StorageFactory creates new storage instances
type StorageFactory interface {
	// NewStorage creates a new storage instance with the given config
	NewStorage(config StorageConfig) (Storage, error)
}

// Common errors
var (
	ErrNotFound = errors.New("environment variable not found")
)
