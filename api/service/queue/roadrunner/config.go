// SPDX-License-Identifier: MPL-2.0

package roadrunner

import (
	"errors"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Kind identifies the RoadRunner queue driver.
const Kind registry.Kind = "queue.driver.roadrunner"

// Config defines the RoadRunner queue driver configuration.
type Config struct {
	// RPCAddr is the RoadRunner RPC address (e.g., "tcp://127.0.0.1:6001").
	RPCAddr string `json:"rpc_addr"`
	// Pipeline is the default RoadRunner pipeline name for publishing.
	Pipeline  string                     `json:"pipeline"`
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.RPCAddr == "" {
		return errors.New("rpc_addr is required")
	}
	return nil
}

// InitDefaults initializes default values.
func (c *Config) InitDefaults() {
	if c.RPCAddr == "" {
		c.RPCAddr = "tcp://127.0.0.1:6001"
	}
}
