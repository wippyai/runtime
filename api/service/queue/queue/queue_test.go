// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/attrs"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
)

func TestKind(t *testing.T) {
	assert.Equal(t, "queue.queue", Kind)
}

func TestConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := &Config{
			Driver: registry.ID{Name: "memory"},
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("missing driver", func(t *testing.T) {
		cfg := &Config{}
		err := cfg.Validate()
		assert.Equal(t, queueapi.ErrDriverIDRequired, err)
	})
}

func TestConfig_InitDefaults(t *testing.T) {
	t.Run("nil options", func(t *testing.T) {
		cfg := &Config{}
		cfg.InitDefaults()
		assert.NotNil(t, cfg.DriverOptions)
	})

	t.Run("existing options", func(t *testing.T) {
		existing := attrs.NewBag()
		existing.Set("key", "value")
		cfg := &Config{DriverOptions: existing}
		cfg.InitDefaults()
		assert.Equal(t, "value", cfg.DriverOptions.GetString("key", ""))
	})
}
