package memory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/registry"
)

func TestKind(t *testing.T) {
	assert.Equal(t, registry.Kind("queue.driver.memory"), Kind)
}

func TestConfig_Validate(t *testing.T) {
	cfg := &Config{}
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestConfig_InitDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.InitDefaults()
	// InitDefaults is a no-op for memory config
}
