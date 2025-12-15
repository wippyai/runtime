// Package sql provides SQL database service configuration.
package sql

import (
	"encoding/json"
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

	// MSSQL identifies a Microsoft SQL Server database in the registry
	MSSQL registry.Kind = "db.sql.mssql"

	// Oracle identifies an Oracle database in the registry
	Oracle registry.Kind = "db.sql.oracle"
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
		MaxOpen     int           `json:"max_open"`     // Maximum number of open connections
		MaxIdle     int           `json:"max_idle"`     // Maximum number of idle connections
		MaxLifetime time.Duration `json:"max_lifetime"` // Maximum lifetime of a connection
	}

	// DBConfig defines the base configuration for SQL databases
	DBConfig struct {
		HostEnv     string `json:"host_env,omitempty"`     // Database host address env variable
		PortEnv     string `json:"port_env,omitempty"`     // Database port number env variable
		DatabaseEnv string `json:"database_env,omitempty"` // Database name env variable
		UsernameEnv string `json:"username_env,omitempty"` // Database user env variable
		PasswordEnv string `json:"password_env,omitempty"` // Database password env variable

		Host      string                     `json:"host"`      // Database host address
		Port      int                        `json:"port"`      // Database port number
		Database  string                     `json:"database"`  // Database name
		Username  string                     `json:"username"`  // Database user
		Password  string                     `json:"password"`  // Database password
		Pool      PoolConfig                 `json:"pool"`      // Connection pool settings
		Options   map[string]string          `json:"options"`   // Database-specific options
		Lifecycle supervisor.LifecycleConfig `json:"lifecycle"` // Lifecycle configuration
	}

	// SQLiteConfig defines SQLite-specific configuration
	SQLiteConfig struct {
		File      string                     `json:"file"`      // Database file path, use :memory: for in-memory database, server fs level
		Pool      PoolConfig                 `json:"pool"`      // Connection pool settings
		Options   map[string]string          `json:"options"`   // SQLite-specific options
		Lifecycle supervisor.LifecycleConfig `json:"lifecycle"` // Lifecycle configuration
	}
)

// UnmarshalJSON provides custom unmarshaling for PoolConfig to handle time.Duration
func (c *PoolConfig) UnmarshalJSON(data []byte) error {
	type Alias PoolConfig
	aux := &struct {
		MaxLifetime string `json:"max_lifetime"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if aux.MaxLifetime != "" {
		duration, err := time.ParseDuration(aux.MaxLifetime)
		if err != nil {
			return NewInvalidDurationError(err)
		}
		c.MaxLifetime = duration
	}

	return nil
}

// MarshalJSON provides custom marshaling for PoolConfig to handle time.Duration
func (c *PoolConfig) MarshalJSON() ([]byte, error) {
	type Alias PoolConfig
	return json.Marshal(&struct {
		MaxLifetime string `json:"max_lifetime"`
		*Alias
	}{
		MaxLifetime: c.MaxLifetime.String(),
		Alias:       (*Alias)(c),
	})
}

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
