// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestParticipantRedirectWire(t *testing.T) {
	body, err := encodeParticipantRedirect(7, "incarnation", "leader", 1024)
	require.NoError(t, err)
	correlation, incarnation, peer, err := decodeParticipantRedirect(body, 1024)
	require.NoError(t, err)
	require.Equal(t, uint64(7), correlation)
	require.Equal(t, "incarnation", incarnation)
	require.Equal(t, "leader", peer)
	_, _, _, err = decodeParticipantRedirect(body, len(body)-1)
	require.ErrorIs(t, err, errParticipantWireTooLarge)
	_, err = encodeParticipantRedirect(7, "incarnation", "leader", len(body)-1)
	require.ErrorIs(t, err, errParticipantWireTooLarge)
	for _, bad := range [][]byte{nil, append(append([]byte(nil), body...), 0), {0x80}} {
		c, _, _, err := decodeParticipantRedirect(bad, 1024)
		require.Error(t, err)
		require.Zero(t, c)
	}
	for _, value := range []map[string]any{
		{"v": uint8(1), "c": uint64(7), "i": "one"},
		{"v": uint8(1), "c": uint64(7), "i": "one", "n": "leader", "extra": true},
		{"v": uint8(2), "c": uint64(7), "i": "one", "n": "leader"},
		{"v": uint8(1), "c": uint64(0), "i": "one", "n": "leader"},
		{"v": uint8(1), "c": uint64(7), "i": "one", "n": ""},
	} {
		encoded, err := encodeParticipantValue(value, 1024)
		require.NoError(t, err)
		c, _, _, err := decodeParticipantRedirect(encoded, 1024)
		require.Error(t, err)
		require.Zero(t, c)
	}
}
