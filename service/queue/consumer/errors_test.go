// SPDX-License-Identifier: MPL-2.0

package consumer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewConcurrencyExceededError(t *testing.T) {
	err := NewConcurrencyExceededError(2000, 1000)
	assert.Contains(t, err.Error(), "concurrency exceeds maximum")
	assert.Equal(t, 2000, err.Details().GetInt("concurrency", 0))
	assert.Equal(t, 1000, err.Details().GetInt("max", 0))
}

func TestNewPrefetchExceededError(t *testing.T) {
	err := NewPrefetchExceededError(20000, 10000)
	assert.Contains(t, err.Error(), "prefetch exceeds maximum")
	assert.Equal(t, 20000, err.Details().GetInt("prefetch", 0))
	assert.Equal(t, 10000, err.Details().GetInt("max", 0))
}
