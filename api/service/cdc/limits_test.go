// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
)

func TestSubscriptionLimits(t *testing.T) {
	defaults := (SubscriptionLimits{}).Effective()
	require.Positive(t, defaults.MaxSubscriptions)
	require.Positive(t, defaults.MaxSnapshotSubscriptions)
	require.Positive(t, defaults.MaxBytes)
	for _, invalid := range []SubscriptionLimits{{MaxSubscriptions: -1}, {MaxSnapshotSubscriptions: -1}, {MaxBytes: -1}} {
		require.ErrorIs(t, invalid.Validate(), ErrInvalidSubscriptionLimits)
	}
}

func TestSnapshotSubscriptionLimitField(t *testing.T) {
	var limits SubscriptionLimits
	require.NoError(t, json.Unmarshal([]byte(`{"max_snapshot_subscriptions":7}`), &limits))
	require.Equal(t, 7, limits.Effective().MaxSnapshotSubscriptions)
}

func TestAdmissionErrorMetadata(t *testing.T) {
	require.Equal(t, apierror.Unavailable, ErrSubscriptionLimit.Kind())
	require.Equal(t, apierror.True, ErrSubscriptionLimit.Retryable())
	require.Equal(t, apierror.Invalid, ErrInvalidSubscriptionLimits.Kind())
	require.Equal(t, apierror.False, ErrInvalidSubscriptionLimits.Retryable())
}
