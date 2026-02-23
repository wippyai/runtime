// SPDX-License-Identifier: MPL-2.0

package metrics

// Config holds metrics service configuration.
type Config struct {
	Interceptor struct {
		Enabled bool `json:"enabled"`
	} `json:"interceptor"`
	Buffer struct {
		Size int `json:"size"`
	} `json:"buffer"`
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Buffer.Size < 0 {
		c.Buffer.Size = 0
	}
	return nil
}

// BufferSize returns the buffer size with default fallback.
func (c *Config) BufferSize() int {
	if c.Buffer.Size == 0 {
		return 10000
	}
	return c.Buffer.Size
}
