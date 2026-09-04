// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionLimits(t *testing.T) {
	defaults := (SubscriptionLimits{}).Effective()
	require.Positive(t, defaults.MaxSubscriptions)
	require.Positive(t, defaults.MaxSnapshots)
	require.Positive(t, defaults.MaxBytes)
	for _, invalid := range []SubscriptionLimits{{MaxSubscriptions: -1}, {MaxSnapshots: -1}, {MaxBytes: -1}} {
		require.ErrorIs(t, invalid.Validate(), ErrInvalidSubscriptionLimits)
	}
}
