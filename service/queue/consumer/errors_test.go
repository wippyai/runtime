package consumer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewConcurrencyExceededError(t *testing.T) {
	err := NewConcurrencyExceededError(2000, 1000)
	assert.Contains(t, err.Error(), "2000")
	assert.Contains(t, err.Error(), "1000")
}

func TestNewPrefetchExceededError(t *testing.T) {
	err := NewPrefetchExceededError(20000, 10000)
	assert.Contains(t, err.Error(), "20000")
	assert.Contains(t, err.Error(), "10000")
}
