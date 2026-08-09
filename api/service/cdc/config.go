// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"time"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

const (
	Postgres registry.Kind = "db.cdc.postgres"
	SQLite   registry.Kind = "db.cdc.sqlite"
)

const (
	OutputPlugin    = "pgoutput"
	ProtocolVersion = 1

	StreamingProtocolVersion = 2

	// These defaults bound decoder memory when the corresponding entry fields
	// are omitted. Zero in Config means "use this default", never unlimited.
	DefaultPostgresMaxTransactionChanges = 1_000_000
	DefaultPostgresMaxTransactionBytes   = 256 << 20
)

type Config struct {
	Options               map[string]string          `json:"options"`
	Database              string                     `json:"database"`
	Password              string                     `json:"password"`
	Host                  string                     `json:"host"`
	Username              string                     `json:"username"`
	SlotName              string                     `json:"slot_name"`
	Publication           string                     `json:"publication,omitempty"`
	StandbyInterval       string                     `json:"standby_interval,omitempty"`
	StatusInterval        string                     `json:"status_interval,omitempty"`
	Tables                []string                   `json:"tables,omitempty"`
	Lifecycle             supervisor.LifecycleConfig `json:"lifecycle"`
	Port                  int                        `json:"port"`
	SnapshotFetchSize     int                        `json:"snapshot_fetch_size,omitempty"`
	MaxTransactionChanges int                        `json:"max_transaction_changes,omitempty"`
	MaxTransactionBytes   int64                      `json:"max_transaction_bytes,omitempty"`
	Temporary             bool                       `json:"temporary,omitempty"`
	Snapshot              bool                       `json:"snapshot,omitempty"`
	Streaming             bool                       `json:"streaming,omitempty"`
	Failover              bool                       `json:"failover,omitempty"`
}

func (c *Config) InitDefaults() {
	if c.Options == nil {
		c.Options = make(map[string]string)
	}
	c.Lifecycle.InitDefaults()
}

func (c *Config) Validate() error {
	if c.Host == "" {
		return ErrHostRequired
	}
	if c.Port <= 0 {
		return ErrInvalidPort
	}
	if c.Database == "" {
		return ErrDatabaseRequired
	}
	if c.Username == "" {
		return ErrUsernameRequired
	}
	if c.Password == "" {
		return ErrPasswordRequired
	}
	if c.SlotName == "" {
		return ErrSlotNameRequired
	}
	if c.Publication == "" && len(c.Tables) == 0 {
		return ErrPublicationRequired
	}
	if c.Failover && c.Temporary {
		return ErrFailoverTemporary
	}
	if c.SnapshotFetchSize < 0 {
		return ErrInvalidSnapshotFetchSize
	}
	if c.MaxTransactionChanges < 0 {
		return ErrInvalidMaxTransactionChanges
	}
	if c.MaxTransactionBytes < 0 {
		return ErrInvalidMaxTransactionBytes
	}
	if _, err := c.StandbyDuration(); err != nil {
		return err
	}
	if _, err := c.StatusDuration(); err != nil {
		return err
	}
	return nil
}

func (c *Config) EffectiveMaxTransactionChanges() int {
	if c.MaxTransactionChanges > 0 {
		return c.MaxTransactionChanges
	}
	return DefaultPostgresMaxTransactionChanges
}

func (c *Config) EffectiveMaxTransactionBytes() int64 {
	if c.MaxTransactionBytes > 0 {
		return c.MaxTransactionBytes
	}
	return DefaultPostgresMaxTransactionBytes
}

func (c *Config) StandbyDuration() (time.Duration, error) {
	return parseInterval(c.StandbyInterval)
}

func (c *Config) StatusDuration() (time.Duration, error) {
	return parseInterval(c.StatusInterval)
}

func parseInterval(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, ErrInvalidInterval
	}
	if d < 0 {
		return 0, ErrInvalidInterval
	}
	return d, nil
}
