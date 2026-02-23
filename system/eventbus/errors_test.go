// SPDX-License-Identifier: MPL-2.0

package eventbus

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewSubscriberError(t *testing.T) {
	cause := context.DeadlineExceeded
	err := NewSubscriberError(cause)
	assert.Contains(t, err.Error(), "failed to create subscriber")
	assert.Equal(t, "Internal", err.Kind().String())
	assert.True(t, err.Retryable().Bool())
	assert.True(t, errors.Is(err, cause))
}
