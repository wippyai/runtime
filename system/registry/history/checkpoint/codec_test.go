// SPDX-License-Identifier: MPL-2.0

package checkpoint

import (
	"context"
	"testing"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
)

func testStateCodec() StateCodec {
	handle := &codec.MsgpackHandle{}
	handle.Canonical = true
	return StateCodec{
		Encode: func(changes registry.ChangeSet) ([]byte, error) {
			var data []byte
			err := codec.NewEncoderBytes(&data, handle).Encode(changes)
			return data, err
		},
		Decode: func(data []byte) (registry.ChangeSet, error) {
			var changes registry.ChangeSet
			err := codec.NewDecoderBytes(data, handle).Decode(&changes)
			return changes, err
		},
	}
}

func TestStateCodecCanonicalRoundTrip(t *testing.T) {
	state := registry.NewReplayState()
	first := registry.Entry{ID: registry.NewID("test", "first"), Kind: "config"}
	second := registry.Entry{ID: registry.NewID("test", "second"), Kind: "config"}
	require.NoError(t, state.Apply(context.Background(), registry.ChangeSet{{Kind: registry.EntryCreate, Entry: first}, {Kind: registry.EntryCreate, Entry: second}}))
	require.NoError(t, state.Apply(context.Background(), registry.ChangeSet{{Kind: registry.EntryDelete, Entry: first}}))
	c := testStateCodec()
	encoded, err := c.EncodeState(state)
	require.NoError(t, err)
	decoded, err := c.DecodeState(encoded)
	require.NoError(t, err)
	require.Equal(t, state, decoded)
	repeated, err := c.EncodeState(decoded)
	require.NoError(t, err)
	require.Equal(t, encoded, repeated)
}

func TestStateCodecRejectsMalformedSupportedPayload(t *testing.T) {
	c := testStateCodec()
	operations, err := c.Encode(nil)
	require.NoError(t, err)
	id := registry.NewID("test", "entry")
	for _, test := range []struct {
		name string
		wire stateWire
	}{
		{name: "negative-entry-count", wire: stateWire{Operations: operations, Entries: -1}},
		{name: "duplicate-tombstone", wire: stateWire{Operations: operations, Tombstones: []registry.ID{id, id}}},
		{name: "invalid-position", wire: stateWire{Operations: operations, Transaction: 1, Sequence: 1, Dependencies: []rangeWire{{ID: id, First: positionWire{Index: 5, Transaction: 1, Sequence: 1}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var data []byte
			require.NoError(t, codec.NewEncoderBytes(&data, &codec.MsgpackHandle{}).Encode(test.wire))
			_, err := c.DecodeState(data)
			require.Error(t, err)
		})
	}
	state := registry.NewReplayState()
	data, err := c.EncodeState(state)
	require.NoError(t, err)
	_, err = c.DecodeState(append(data, 0x90))
	require.ErrorContains(t, err, "trailing bytes")
	_, err = c.DecodeState([]byte{0xc1})
	require.Error(t, err)
}
