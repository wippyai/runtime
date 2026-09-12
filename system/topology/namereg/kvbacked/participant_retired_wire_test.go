// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParticipantRetirementWireRefusalIsDistinctAndBound(t *testing.T) {
	encoded, err := encodeParticipantFailure(42, "old-incarnation", fmt.Errorf("enrollment: %w", ErrParticipantRetired), 1024)
	require.NoError(t, err)
	correlation, incarnation, err := decodeParticipantFailure(encoded, 1024)
	require.ErrorIs(t, err, ErrParticipantRetired)
	require.NotErrorIs(t, err, errParticipantUnavailable)
	require.Equal(t, uint64(42), correlation)
	require.Equal(t, "old-incarnation", incarnation)
}
