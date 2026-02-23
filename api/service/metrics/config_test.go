// SPDX-License-Identifier: MPL-2.0

package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig_Validate(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		cfg := Config{}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("negative buffer size", func(t *testing.T) {
		cfg := Config{}
		cfg.Buffer.Size = -100
		err := cfg.Validate()
		assert.NoError(t, err)
		assert.Equal(t, 0, cfg.Buffer.Size)
	})

	t.Run("valid buffer size", func(t *testing.T) {
		cfg := Config{}
		cfg.Buffer.Size = 5000
		err := cfg.Validate()
		assert.NoError(t, err)
		assert.Equal(t, 5000, cfg.Buffer.Size)
	})
}

func TestConfig_BufferSize(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		cfg := Config{}
		assert.Equal(t, 10000, cfg.BufferSize())
	})

	t.Run("custom", func(t *testing.T) {
		cfg := Config{}
		cfg.Buffer.Size = 5000
		assert.Equal(t, 5000, cfg.BufferSize())
	})
}
