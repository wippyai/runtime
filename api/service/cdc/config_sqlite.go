// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"time"

	"github.com/wippyai/runtime/api/supervisor"
)

type SQLiteConfig struct {
	DBResource     string                     `json:"db_resource"`
	Name           string                     `json:"name,omitempty"`
	StatusInterval string                     `json:"status_interval,omitempty"`
	Tables         []string                   `json:"tables,omitempty"`
	Lifecycle      supervisor.LifecycleConfig `json:"lifecycle"`
	Snapshot       bool                       `json:"snapshot,omitempty"`
}

func (c *SQLiteConfig) InitDefaults() {
	c.Lifecycle.InitDefaults()
}

func (c *SQLiteConfig) Validate() error {
	if c.DBResource == "" {
		return ErrDBResourceRequired
	}
	if _, err := c.StatusDuration(); err != nil {
		return err
	}
	return nil
}

func (c *SQLiteConfig) StatusDuration() (time.Duration, error) {
	return parseInterval(c.StatusInterval)
}
