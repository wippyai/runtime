package sql

import (
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
)

// Registry kind constants for different SQL database types
const (
	// KindPostgres identifies a PostgreSQL database in the registry
	KindPostgres registry.Kind = "db.sql.postgres"

	// KindMySQL identifies a MySQL database in the registry
	KindMySQL registry.Kind = "db.sql.mysql"

	// KindSQLite identifies a SQLite database in the registry
	KindSQLite registry.Kind = "db.sql.sqlite"

	// KindMSSQL identifies a Microsoft SQL Server database in the registry
	KindMSSQL registry.Kind = "db.sql.mssql"

	// KindOracle identifies an Oracle database in the registry
	KindOracle registry.Kind = "db.sql.oracle"
)

type (
	// PoolConfig defines settings for a database connection pool
	PoolConfig struct {
		MaxOpen     int    `json:"max_open"`     // Maximum number of open connections
		MaxIdle     int    `json:"max_idle"`     // Maximum number of idle connections
		MaxLifetime string `json:"max_lifetime"` // Maximum lifetime of a connection
	}

	// DBConfig defines the base configuration for SQL databases
	DBConfig struct {
		Host      string            `json:"host"`     // Database host address
		Port      int               `json:"port"`     // Database port number
		Database  string            `json:"database"` // Database name
		Username  string            `json:"username"` // Database user
		Password  string            `json:"password"` // Database password
		Pool      PoolConfig        `json:"pool"`     // Connection pool settings
		Options   map[string]string `json:"options"`  // Database-specific options
		Lifecycle supervisor.LifecycleConfig
	}

	// SQLiteConfig defines SQLite-specific configuration
	SQLiteConfig struct {
		File      string            `json:"file"`    // Database file path, use memory for in-memory database
		FS        registry.ID       `json:"fs"`      // Optional filesystem resource ID
		Pool      PoolConfig        `json:"pool"`    // Connection pool settings
		Options   map[string]string `json:"options"` // SQLite-specific options
		Lifecycle supervisor.LifecycleConfig
	}
)

// Validate checks if the DBConfig has all required fields set to valid values
func (c *DBConfig) Validate() error {
	if c.Host == "" {
		return fmt.Errorf("host is required")
	}

	if c.Port <= 0 {
		return fmt.Errorf("port must be greater than 0")
	}

	if c.Database == "" {
		return fmt.Errorf("database is required")
	}

	if c.Username == "" {
		return fmt.Errorf("username is required")
	}

	if c.Password == "" {
		return fmt.Errorf("password is required")
	}

	if c.Pool.MaxOpen <= 0 {
		return fmt.Errorf("pool.max_open must be greater than 0")
	}

	if c.Pool.MaxIdle < 0 {
		return fmt.Errorf("pool.max_idle must be greater than or equal to 0")
	}

	// todo: make it duration
	if c.Pool.MaxLifetime == "" {
		return fmt.Errorf("pool.max_lifetime is required")
	}

	return nil
}

// Validate checks if the SQLiteConfig has all required fields set to valid values
func (c *SQLiteConfig) Validate() error {
	if c.File == "" {
		return fmt.Errorf("file is required")
	}

	if c.FS.Name == "" && c.File != ":memory:" {
		return fmt.Errorf("filesystem (fs) is required")
	}

	return nil
}
