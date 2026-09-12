// SPDX-License-Identifier: MPL-2.0

package global

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJoinEncodingBounds(t *testing.T) {
	for _, entries := range [][]joinEntryEnvelope{nil, {}, {{Name: "name", State: joinSnapshotStatePending}}} {
		original := &joinResponseEnvelope{CorrID: 7, StrongIndex: 9, Entries: entries}
		body, err := marshalMsgpack(original)
		require.NoError(t, err)
		encoded, err := encodeJoinSnapshot(original, len(body))
		require.NoError(t, err)
		require.Equal(t, body, encoded)
		cfg := DefaultJoinConfig()
		cfg.MaxBytes = len(body)
		decoded, err := decodeJoinSnapshot(encoded, cfg)
		require.NoError(t, err)
		require.Equal(t, original, decoded)
		encoded, err = encodeJoinSnapshot(original, len(body)-1)
		require.ErrorIs(t, err, ErrJoinSnapshotTooLarge)
		require.Nil(t, encoded)
		cfg.MaxBytes--
		decoded, err = decodeJoinSnapshot(body, cfg)
		require.ErrorIs(t, err, ErrJoinSnapshotTooLarge)
		require.Nil(t, decoded)
	}
}

func TestJoinEntryLimitRejectsWholeCapture(t *testing.T) {
	s := newJoinTestService(t)
	p := makePID("node-1", "host", "owner")
	applyAt(t, s.fsm, &Command{Type: CmdRegister, Name: "first", PID: p}, 1)
	applyAt(t, s.fsm, &Command{Type: CmdRegisterPending, Name: "second", PID: p, RequiredNodes: []string{p.Node}}, 2)
	snapshot, err := s.buildJoinSnapshot(0, 1)
	require.ErrorIs(t, err, ErrJoinSnapshotTooLarge)
	require.Nil(t, snapshot)
	snapshot, err = s.buildJoinSnapshot(0, 2)
	require.NoError(t, err)
	body, err := marshalMsgpack(snapshot)
	require.NoError(t, err)
	cfg := DefaultJoinConfig()
	cfg.MaxEntries = 1
	snapshot, err = decodeJoinSnapshot(body, cfg)
	require.ErrorIs(t, err, ErrJoinSnapshotTooLarge)
	require.Nil(t, snapshot)
}

func TestJoinSlotsBoundWorkAndReleaseAfterFailure(t *testing.T) {
	s := newJoinTestService(t)
	cfg := DefaultJoinConfig()
	cfg.MaxConcurrent = 1
	cfg.MaxBytes = 1
	require.NoError(t, s.SetJoinConfig(cfg))
	_, release, err := s.acquireJoin()
	require.NoError(t, err)
	snapshot, err := s.captureJoinSnapshot(0)
	require.ErrorIs(t, err, ErrJoinBusy)
	require.Nil(t, snapshot)
	require.Error(t, s.SetJoinConfig(cfg))
	release()
	snapshot, err = s.captureJoinSnapshot(0)
	require.Error(t, err, "byte limit must reject even empty envelope")
	require.Nil(t, snapshot)
	_, release, err = s.acquireJoin()
	require.NoError(t, err, "failed encoding must release its slot")
	release()
}

func TestJoinMalformedEnvelopeRejected(t *testing.T) {
	cfg := DefaultJoinConfig()
	bodies := [][]byte{
		{0x80}, // no mandatory fields
		{0x83, 0xa2, 'e', 'n', 0xdd, 0xff, 0xff, 0xff, 0xff}, // huge truncated array
		{0xdf, 0xff, 0xff, 0xff, 0xff},                       // huge envelope map
	}
	valid, err := marshalMsgpack(&joinResponseEnvelope{})
	require.NoError(t, err)
	bodies = append(bodies, append(valid, 0xc0)) // trailing object
	for _, body := range bodies {
		snapshot, err := decodeJoinSnapshot(body, cfg)
		require.Error(t, err)
		require.Nil(t, snapshot)
	}
}

func TestJoinWriterCapacityIsBounded(t *testing.T) {
	writer := &boundedJoinWriter{limit: 100}
	for _, size := range []int{70, 30} {
		n, err := writer.Write(make([]byte, size))
		require.NoError(t, err)
		require.Equal(t, size, n)
		require.LessOrEqual(t, cap(writer.data), writer.limit)
	}
	n, err := writer.Write([]byte{1})
	require.ErrorIs(t, err, ErrJoinSnapshotTooLarge)
	require.Zero(t, n)
	require.Len(t, writer.data, 100)
}
