// Package cluster provides a minimal façade over the Raft-backed key/value state machine.
package cluster

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrCAS(t *testing.T) {
	t.Run("error message", func(t *testing.T) {
		assert.Equal(t, "compare-and-swap failed", ErrCAS.Error())
	})

	t.Run("error comparison", func(t *testing.T) {
		err := ErrCAS
		assert.True(t, errors.Is(err, ErrCAS))
	})

	t.Run("not equal to other errors", func(t *testing.T) {
		otherErr := errors.New("different error")
		assert.False(t, errors.Is(otherErr, ErrCAS))
	})
}
