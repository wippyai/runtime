// SPDX-License-Identifier: MPL-2.0
package temporal

import (
	"fmt"
	"time"
)

// MonitorConfig bounds native provider observation and completion retention.
type MonitorConfig struct {
	MaxTargets      int           `json:"max_targets,omitempty"`
	MaxObservers    int           `json:"max_observers,omitempty"`
	DeliveryTimeout time.Duration `json:"delivery_timeout,omitempty"`
}

func (c *MonitorConfig) InitDefaults() {
	if c.MaxTargets == 0 {
		c.MaxTargets = 4096
	}
	if c.MaxObservers == 0 {
		c.MaxObservers = 256
	}
	if c.DeliveryTimeout == 0 {
		c.DeliveryTimeout = 5 * time.Second
	}
}
func (c MonitorConfig) Validate() error {
	if c.MaxTargets <= 0 || c.MaxObservers <= 0 || c.DeliveryTimeout <= 0 {
		return fmt.Errorf("Temporal monitor bounds and delivery timeout must be positive")
	}
	return nil
}
