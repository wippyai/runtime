// SPDX-License-Identifier: MPL-2.0

package store

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	apierror "github.com/wippyai/runtime/api/error"
)

func TestErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{"store full", ErrStoreFull, "store is full"},
		{"store closed", ErrStoreClosed, "store is closed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
			assert.True(t, errors.Is(tt.err, tt.err))
		})
	}
}

func TestError_Interface(t *testing.T) {
	t.Run("ErrStoreFull", func(t *testing.T) {
		assert.Equal(t, apierror.Unavailable, ErrStoreFull.Kind())
		assert.Equal(t, apierror.True, ErrStoreFull.Retryable())
	})

	t.Run("ErrStoreClosed", func(t *testing.T) {
		assert.Equal(t, apierror.Unavailable, ErrStoreClosed.Kind())
		assert.Equal(t, apierror.False, ErrStoreClosed.Retryable())
	})
}
