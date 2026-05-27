// SPDX-License-Identifier: MPL-2.0

package consumer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
)

func TestConstants(t *testing.T) {
	assert.Equal(t, 1, DefaultConcurrency)
	assert.Equal(t, 10, DefaultPrefetch)
	assert.Equal(t, 1000, MaxConcurrency)
	assert.Equal(t, 10000, MaxPrefetch)
}

func newCfg(opts queueapi.ConsumerOptions) *Config {
	return &Config{ConsumerOptions: opts}
}

func TestConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := newCfg(queueapi.ConsumerOptions{
			Queue:       registry.ID{Name: "test-queue"},
			Func:        registry.ID{Name: "test-func"},
			Concurrency: 5,
			Prefetch:    100,
		})
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("missing queue", func(t *testing.T) {
		cfg := newCfg(queueapi.ConsumerOptions{
			Func: registry.ID{Name: "test-func"},
		})
		err := cfg.Validate()
		assert.Equal(t, ErrQueueIDRequired, err)
	})

	t.Run("missing func", func(t *testing.T) {
		cfg := newCfg(queueapi.ConsumerOptions{
			Queue: registry.ID{Name: "test-queue"},
		})
		err := cfg.Validate()
		assert.Equal(t, ErrFunctionIDRequired, err)
	})

	t.Run("defaults concurrency", func(t *testing.T) {
		cfg := newCfg(queueapi.ConsumerOptions{
			Queue:       registry.ID{Name: "test-queue"},
			Func:        registry.ID{Name: "test-func"},
			Concurrency: 0,
		})
		err := cfg.Validate()
		assert.NoError(t, err)
		assert.Equal(t, DefaultConcurrency, cfg.Concurrency)
	})

	t.Run("defaults prefetch", func(t *testing.T) {
		cfg := newCfg(queueapi.ConsumerOptions{
			Queue:    registry.ID{Name: "test-queue"},
			Func:     registry.ID{Name: "test-func"},
			Prefetch: 0,
		})
		err := cfg.Validate()
		assert.NoError(t, err)
		assert.Equal(t, DefaultPrefetch, cfg.Prefetch)
	})

	t.Run("concurrency exceeds max", func(t *testing.T) {
		cfg := newCfg(queueapi.ConsumerOptions{
			Queue:       registry.ID{Name: "test-queue"},
			Func:        registry.ID{Name: "test-func"},
			Concurrency: MaxConcurrency + 1,
		})
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "concurrency")
		assert.Contains(t, err.Error(), "exceeds maximum")
	})

	t.Run("prefetch exceeds max", func(t *testing.T) {
		cfg := newCfg(queueapi.ConsumerOptions{
			Queue:    registry.ID{Name: "test-queue"},
			Func:     registry.ID{Name: "test-func"},
			Prefetch: MaxPrefetch + 1,
		})
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "prefetch")
		assert.Contains(t, err.Error(), "exceeds maximum")
	})

	t.Run("negative concurrency gets default", func(t *testing.T) {
		cfg := newCfg(queueapi.ConsumerOptions{
			Queue:       registry.ID{Name: "test-queue"},
			Func:        registry.ID{Name: "test-func"},
			Concurrency: -1,
		})
		err := cfg.Validate()
		assert.NoError(t, err)
		assert.Equal(t, DefaultConcurrency, cfg.Concurrency)
	})

	t.Run("negative prefetch gets default", func(t *testing.T) {
		cfg := newCfg(queueapi.ConsumerOptions{
			Queue:    registry.ID{Name: "test-queue"},
			Func:     registry.ID{Name: "test-func"},
			Prefetch: -1,
		})
		err := cfg.Validate()
		assert.NoError(t, err)
		assert.Equal(t, DefaultPrefetch, cfg.Prefetch)
	})
}

func TestErrors(t *testing.T) {
	assert.Equal(t, "queue ID is required", ErrQueueIDRequired.Error())
	assert.Equal(t, "function ID is required", ErrFunctionIDRequired.Error())
}
