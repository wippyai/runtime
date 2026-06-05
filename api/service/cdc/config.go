// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

const (
	Postgres registry.Kind = "db.cdc.postgres"
)

const (
	DefaultEventSystem = "postgres_cdc"
	ChangeKind         = "change"
	StatusKind         = "status"
	ErrorKind          = "error"
	OutputPlugin       = "pgoutput"
	ProtocolVersion    = 1
)

type Config struct {
	Options     map[string]string          `json:"options"`
	Database    string                     `json:"database"`
	Password    string                     `json:"password"`
	Host        string                     `json:"host"`
	Username    string                     `json:"username"`
	HostEnv     string                     `json:"host_env,omitempty"`
	PortEnv     string                     `json:"port_env,omitempty"`
	DatabaseEnv string                     `json:"database_env,omitempty"`
	UsernameEnv string                     `json:"username_env,omitempty"`
	PasswordEnv string                     `json:"password_env,omitempty"`
	SlotName    string                     `json:"slot_name"`
	Publication string                     `json:"publication,omitempty"`
	EventSystem string                     `json:"event_system,omitempty"`
	Tables      []string                   `json:"tables,omitempty"`
	Lifecycle   supervisor.LifecycleConfig `json:"lifecycle"`
	Port        int                        `json:"port"`
	Temporary   bool                       `json:"temporary,omitempty"`
	Snapshot    bool                       `json:"snapshot,omitempty"`
}

func (c *Config) InitDefaults() {
	if c.Options == nil {
		c.Options = make(map[string]string)
	}
	if c.EventSystem == "" {
		c.EventSystem = DefaultEventSystem
	}
	c.Lifecycle.InitDefaults()
}

func (c *Config) Validate() error {
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
	if c.SlotName == "" {
		return ErrSlotNameRequired
	}
	if c.Publication == "" && len(c.Tables) == 0 {
		return ErrPublicationRequired
	}
	return nil
}
