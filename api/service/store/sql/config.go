// Package sqlstore provides SQL-backed store service configuration.
package sql

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Registry kind constant for the SQL KV store
const (
	// KindSQLKV identifies a SQL KV store in the registry
	KindSQLKV registry.Kind = "store.sql"
)

// SQLConfig provides configuration for a SQL-based key-value store with TTL support.
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
		return ErrDatabaseIDRequired
	}

	// Table name must be specified
	if c.TableName == "" {
		return ErrTableNameRequired
	}

	// ID column name must be specified
	if c.IDColumnName == "" {
		return ErrIDColumnNameRequired
	}

	// Payload column name must be specified
	if c.PayloadColumnName == "" {
		return ErrPayloadColumnNameRequired
	}

	// Expire column name must be specified
	if c.ExpireColumnName == "" {
		return ErrExpireColumnNameRequired
	}

	// CleanupInterval must be non-negative (0 means no cleanup)
	if c.CleanupInterval < 0 {
		return ErrCleanupIntervalInvalid
	}

	// Validate the database ID and table name for SQL injection prevention
	if !c.IsSafe(c.Database.Name) {
		return ErrDatabaseIDInvalid
	}

	// Validate the table name and column names for SQL injection prevention
	if !c.IsSafe(c.TableName) {
		return ErrTableNameInvalid
	}

	// Validate the column names for SQL injection prevention
	if !c.IsSafe(c.IDColumnName) {
		return ErrIDColumnNameInvalid
	}

	// Validate the column names for SQL injection prevention
	if !c.IsSafe(c.PayloadColumnName) {
		return ErrPayloadColumnNameInvalid
	}

	// Validate the column names for SQL injection prevention
	if !c.IsSafe(c.ExpireColumnName) {
		return ErrExpireColumnNameInvalid
	}

	return nil
}

// InitDefaults sets sensible defaults for the SQL store configuration.
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

// UnmarshalJSON deserializes SQLConfig from JSON, parsing duration strings.
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
			return NewInvalidCleanupIntervalError(err)
		}
	}

	return nil
}

// MarshalJSON serializes SQLConfig to JSON.
func (c *SQLConfig) MarshalJSON() ([]byte, error) {
	type Alias SQLConfig
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(c),
	})
}

// IsSafe validates that the input string is safe for SQL use and not an injection attempt.
func (c *SQLConfig) IsSafe(input string) bool {
	// Check for SQL reserved words that might indicate an injection attempt
	reservedWords := map[string]bool{
		"select": true, "from": true, "where": true, "insert": true,
		"update": true, "delete": true, "table": true, "drop": true,
		"group": true, "order": true, "by": true, "into": true,
		"values": true, "limit": true, "offset": true, "join": true,
		"union": true, "having": true, "create": true, "alter": true,
		"index": true, "distinct": true, "exists": true, "and": true,
		"or": true, "not": true, "as": true,
	}

	// If input contains SQL syntax characters or patterns, flag as potential injection
	sqlPatterns := []string{
		`'.*'`,           // String literal
		`--`,             // SQL comment
		`;`,              // Statement terminator
		`/\*.*\*/`,       // Block comment
		`union\s+select`, // UNION SELECT
		`drop\s+table`,   // DROP TABLE
		`delete\s+from`,  // DELETE FROM
	}

	if len(input) > 63 {
		return false
	}
	// Check input against SQL patterns
	for _, pattern := range sqlPatterns {
		matched, _ := regexp.MatchString(pattern, strings.ToLower(input))
		if matched {
			return false
		}
	}

	// Check if input contains SQL reserved words
	words := strings.Fields(strings.ToLower(input))
	for _, word := range words {
		// Clean the word of any punctuation
		cleanWord := regexp.MustCompile(`[^\w]`).ReplaceAllString(word, "")
		if reservedWords[cleanWord] {
			return false
		}
	}

	// Check for valid identifier pattern
	// If it's NOT a valid identifier, it might be an injection attempt
	if matched, _ := regexp.MatchString(`^[a-zA-Z][a-zA-Z0-9_]*$`, input); !matched {
		return false
	}

	// Count quotes - an odd number or multiple quotes may indicate injection
	if strings.Count(input, "'") > 0 || strings.Count(input, "\"") > 0 {
		return false
	}

	// Input appears safe
	return true
}
