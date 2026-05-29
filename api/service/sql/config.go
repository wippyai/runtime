// SPDX-License-Identifier: MPL-2.0

// Package sql provides SQL database service configuration.
package sql

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Registry kind constants for different SQL database types
const (
	// Postgres identifies a PostgreSQL database in the registry
	Postgres registry.Kind = "db.sql.postgres"

	// MySQL identifies a MySQL database in the registry
	MySQL registry.Kind = "db.sql.mysql"

	// SQLite identifies a SQLite database in the registry
	SQLite registry.Kind = "db.sql.sqlite"
)

// Default configuration values
const (
	// DefaultMaxOpen is the default maximum number of open connections
	DefaultMaxOpen = 0

	// DefaultMaxIdle is the default maximum number of idle connections
	DefaultMaxIdle = 0

	// DefaultMaxLifetime is the default maximum lifetime of a connection
	DefaultMaxLifetime = 1 * time.Hour
)

type (
	// PoolConfig defines settings for a database connection pool
	PoolConfig struct {
		MaxOpen     int           `json:"max_open"`                           // Maximum number of open connections
		MaxIdle     int           `json:"max_idle"`                           // Maximum number of idle connections
		MaxLifetime time.Duration `json:"max_lifetime,omitzero,format:units"` // Maximum lifetime of a connection
	}

	// DBConfig defines the base configuration for SQL databases
	DBConfig struct {
		Options     map[string]string          `json:"options"`
		Database    string                     `json:"database"`
		Password    string                     `json:"password"`
		UsernameEnv string                     `json:"username_env,omitempty"`
		PasswordEnv string                     `json:"password_env,omitempty"`
		Host        string                     `json:"host"`
		Username    string                     `json:"username"`
		HostEnv     string                     `json:"host_env,omitempty"`
		DatabaseEnv string                     `json:"database_env,omitempty"`
		PortEnv     string                     `json:"port_env,omitempty"`
		Lifecycle   supervisor.LifecycleConfig `json:"lifecycle"`
		Pool        PoolConfig                 `json:"pool"`
		Port        int                        `json:"port"`
	}

	// SQLiteConfig defines SQLite-specific configuration
	SQLiteConfig struct {
		Options   map[string]string          `json:"options"`
		File      string                     `json:"file"`
		Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
		Pool      PoolConfig                 `json:"pool"`
	}
)

// InitDefaults initializes the PoolConfig with default values if not set
func (c *PoolConfig) InitDefaults() {
	if c.MaxOpen <= 0 {
		c.MaxOpen = DefaultMaxOpen
	}
	if c.MaxIdle <= 0 {
		c.MaxIdle = DefaultMaxIdle
	}
	if c.MaxLifetime <= 0 {
		c.MaxLifetime = DefaultMaxLifetime
	}
}

// InitDefaults initializes the DBConfig with default values if not set
func (c *DBConfig) InitDefaults() {
	// Initialize pool configuration
	c.Pool.InitDefaults()

	// Initialize options map if nil
	if c.Options == nil {
		c.Options = make(map[string]string)
	}

	// Initialize lifecycle defaults
	c.Lifecycle.InitDefaults()
}

// InitDefaults initializes the SQLiteConfig with default values if not set
func (c *SQLiteConfig) InitDefaults() {
	// Initialize pool configuration
	c.Pool.InitDefaults()

	// Initialize options map if nil
	if c.Options == nil {
		c.Options = make(map[string]string)
	}

	// Initialize lifecycle defaults
	c.Lifecycle.InitDefaults()
}

// Validate checks if the DBConfig has all required fields set to valid values
func (c *DBConfig) Validate() error {
	if c.Host == "" && c.HostEnv == "" {
		return ErrHostRequired
	}

	if c.Port <= 0 && c.PortEnv == "" {
		return ErrInvalidPort
	}

	if c.Database == "" && c.DatabaseEnv == "" {
		return ErrDatabaseRequired
	}

	if c.Username == "" && c.UsernameEnv == "" {
		return ErrUsernameRequired
	}

	if c.Password == "" && c.PasswordEnv == "" {
		return ErrPasswordRequired
	}

	if c.Pool.MaxOpen < 0 {
		return ErrInvalidMaxOpen
	}

	if c.Pool.MaxIdle < 0 {
		return ErrInvalidMaxIdle
	}

	if c.Pool.MaxLifetime <= 0 {
		return ErrInvalidMaxLifetime
	}

	return nil
}

// Validate checks if the SQLiteConfig has all required fields set to valid values
func (c *SQLiteConfig) Validate() error {
	if c.File == "" {
		return ErrFileRequired
	}

	if c.Pool.MaxLifetime <= 0 {
		return ErrInvalidMaxLifetime
	}

	return nil
}

// MarshalJSON implements custom marshaling for PoolConfig to output durations as strings.
func (c *PoolConfig) MarshalJSON() ([]byte, error) {
	type alias struct {
		MaxLifetime string `json:"max_lifetime,omitempty"`
		MaxOpen     int    `json:"max_open"`
		MaxIdle     int    `json:"max_idle"`
	}
	a := alias{
		MaxOpen: c.MaxOpen,
		MaxIdle: c.MaxIdle,
	}
	if c.MaxLifetime != 0 {
		a.MaxLifetime = c.MaxLifetime.String()
	}
	return json.Marshal(a)
}

// UnmarshalJSON implements custom unmarshaling for PoolConfig to parse duration strings.
func (c *PoolConfig) UnmarshalJSON(data []byte) error {
	type alias struct {
		MaxLifetime string `json:"max_lifetime"`
		MaxOpen     int    `json:"max_open"`
		MaxIdle     int    `json:"max_idle"`
	}
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	c.MaxOpen = a.MaxOpen
	c.MaxIdle = a.MaxIdle
	if a.MaxLifetime != "" {
		d, err := time.ParseDuration(a.MaxLifetime)
		if err != nil {
			return fmt.Errorf("invalid max_lifetime: %w", err)
		}
		c.MaxLifetime = d
	}
	return nil
}
