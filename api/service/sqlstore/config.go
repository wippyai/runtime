package sqlstore

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
)

// Registry kind constant for the SQL KV store
const (
	// KindSQLKV identifies a SQL KV store in the registry
	KindSQLKV registry.Kind = "store.sql"
)

// SQLConfig defines configuration for a SQL-based key-value store
type SQLConfig struct {
	// Database is the ID of the database resource to use
	Database registry.ID `json:"database"`

	// TableName is the name of the table to use for storage
	TableName string `json:"table_name"`

	// IDColumnName is the name of the column used for storing keys
	IDColumnName string `json:"id_column_name"`

	// PayloadColumnName is the name of the column used for storing values
	PayloadColumnName string `json:"payload_column_name"`

	// ExpireColumnName is the name of the column used for storing expiration timestamps
	ExpireColumnName string `json:"expire_column_name"`

	// CleanupInterval is how often the store checks for expired entries
	// The store will run a background task at this interval to remove entries with expired TTLs
	// Set to 0 to disable automatic cleanup
	CleanupInterval time.Duration `json:"cleanup_interval"`

	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
}

// Validate checks if the configuration is valid
// Returns an error if any configuration values are invalid
func (c *SQLConfig) Validate() error {
	// Database ID must be valid
	if c.Database.Name == "" {
		return fmt.Errorf("database ID is required")
	}

	// Table name must be specified
	if c.TableName == "" {
		return fmt.Errorf("table_name is required")
	}

	// ID column name must be specified
	if c.IDColumnName == "" {
		return fmt.Errorf("id_column_name is required")
	}

	// Payload column name must be specified
	if c.PayloadColumnName == "" {
		return fmt.Errorf("payload_column_name is required")
	}

	// Expire column name must be specified
	if c.ExpireColumnName == "" {
		return fmt.Errorf("expire_column_name is required")
	}

	// CleanupInterval must be non-negative (0 means no cleanup)
	if c.CleanupInterval < 0 {
		return fmt.Errorf("cleanup_interval must be greater than or equal to 0")
	}

	return nil
}

// InitDefaults initializes the configuration with sensible defaults
// Called during configuration loading to ensure all values have reasonable defaults
func (c *SQLConfig) InitDefaults() {
	// Set default column names if not specified
	if c.IDColumnName == "" {
		c.IDColumnName = "key"
	}

	if c.PayloadColumnName == "" {
		c.PayloadColumnName = "value"
	}

	if c.ExpireColumnName == "" {
		c.ExpireColumnName = "expires_at"
	}

	c.Lifecycle.InitDefaults()
}

// UnmarshalJSON implements custom unmarshaling for SQLConfig
func (c *SQLConfig) UnmarshalJSON(data []byte) error {
	type Alias SQLConfig
	aux := &struct {
		CleanupInterval string `json:"cleanup_interval"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	var err error
	if aux.CleanupInterval != "" {
		c.CleanupInterval, err = time.ParseDuration(aux.CleanupInterval)
		if err != nil {
			return fmt.Errorf("invalid CleanupInterval duration format: %w", err)
		}
	}

	return nil
}

// MarshalJSON implements custom marshaling for SQLConfig
func (c *SQLConfig) MarshalJSON() ([]byte, error) {
	type Alias SQLConfig
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(c),
	})
}
