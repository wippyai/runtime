// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"testing"

	"github.com/stretchr/testify/require"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func TestParticipantConditionalWireRoundTripAndInvalidCombinations(t *testing.T) {
	request := &participantWireRequest{Version: participantWireVersion, Correlation: 7, Incarnation: "client-one", Lifetime: "authority-one", KnownRevision: 42}
	body, err := encodeParticipantRequest(request, 4096)
	require.NoError(t, err)
	decoded, err := decodeParticipantRequest(body, 4096)
	require.NoError(t, err)
	require.Equal(t, request, decoded)
	for _, bad := range []participantWireRequest{
		{Version: 1, Correlation: 7, Incarnation: "client-one", Lifetime: "authority-one"},
		{Version: 1, Correlation: 7, Incarnation: "client-one", KnownRevision: 42},
	} {
		_, err := encodeParticipantRequest(&bad, 4096)
		require.ErrorIs(t, err, errParticipantWireInvalid)
		raw, err := encodeParticipantValue(&bad, 4096)
		require.NoError(t, err)
		_, err = decodeParticipantRequest(raw, 4096)
		require.ErrorIs(t, err, errParticipantWireInvalid)
	}
	response := &participantWireResponse{Version: 1, Correlation: 7, Incarnation: "client-one", Lifetime: "authority-one", Revision: 42, Unchanged: true, Entries: []kvapi.Entry{}}
	body, err = encodeParticipantResponse(response, 4096, 4)
	require.NoError(t, err)
	got, err := decodeParticipantResponse(body, 4096, 4)
	require.NoError(t, err)
	require.Equal(t, response, got)
	fields, err := participantWireMapFields(body, 7)
	require.NoError(t, err)
	for _, test := range []struct {
		key   string
		value any
	}{
		{"g", ""}, {"u", uint8(2)}, {"u", true}, {"r", uint64(0)},
	} {
		var changed []participantWireField
		for _, key := range []string{"v", "c", "i", "g", "r", "u", "e"} {
			value := fields[key]
			if key == test.key {
				value = participantWireRaw(test.value)
			}
			changed = append(changed, participantWireField{key, value})
		}
		_, err := decodeParticipantResponse(participantWireMap(changed...), 4096, 4)
		require.Error(t, err, "invalid %s", test.key)
	}
	response.Entries = []kvapi.Entry{{Key: "not-empty", Value: []byte("value"), Version: 1}}
	_, err = encodeParticipantResponse(response, 4096, 4)
	require.ErrorIs(t, err, errParticipantWireInvalid)
	response.Unchanged = false
	body, err = encodeParticipantResponse(response, 4096, 4)
	require.NoError(t, err)
	fields, err = participantWireMapFields(body, 7)
	require.NoError(t, err)
	var nonempty []participantWireField
	for _, key := range []string{"v", "c", "i", "g", "r", "u", "e"} {
		value := fields[key]
		if key == "u" {
			value = participantWireRaw(uint8(1))
		}
		nonempty = append(nonempty, participantWireField{key, value})
	}
	_, err = decodeParticipantResponse(participantWireMap(nonempty...), 4096, 4)
	require.ErrorIs(t, err, errParticipantWireInvalid)
}
