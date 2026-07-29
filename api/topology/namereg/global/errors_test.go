// SPDX-License-Identifier: MPL-2.0

package global

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestA27StrongTimeoutMetadata(t *testing.T) {
	err := &StrongRegistrationTimeoutError{
		Name:        "orders/primary",
		MissingAcks: []string{"node-east", "node-west"},
		Epoch:       4097,
	}

	assert.ErrorIs(t, err, ErrStrongRegistrationTimeout)
	assert.False(t, errors.Is(err, ErrStrongRegistrationRejected))
	assert.Equal(t, "orders/primary", err.Name)
	assert.Equal(t, []string{"node-east", "node-west"}, err.MissingAcks)
	assert.Equal(t, uint64(4097), err.Epoch)
	assert.Equal(t, "strong registration timed out before all live nodes acked (name=orders/primary)", err.Error())
	var typed *StrongRegistrationTimeoutError
	require.ErrorAs(t, err, &typed)
	assert.Same(t, err, typed)
}

func TestA28StrongConflictMetadata(t *testing.T) {
	err := &StrongConflictError{
		Name:       "orders/primary",
		Reason:     "owned by another process",
		RejectedBy: "node-central",
		Epoch:      4098,
	}

	assert.ErrorIs(t, err, ErrStrongRegistrationRejected)
	assert.False(t, errors.Is(err, ErrStrongRegistrationTimeout))
	assert.Equal(t, "orders/primary", err.Name)
	assert.Equal(t, "owned by another process", err.Reason)
	assert.Equal(t, "node-central", err.RejectedBy)
	assert.Equal(t, uint64(4098), err.Epoch)
	assert.Equal(t, "strong registration rejected by a required node (name=orders/primary by=node-central)", err.Error())
	var typed *StrongConflictError
	require.ErrorAs(t, err, &typed)
	assert.Same(t, err, typed)
}
