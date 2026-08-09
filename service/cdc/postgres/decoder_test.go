// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"testing"

	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func textTuple(vals ...string) *pglogrepl.TupleData {
	cols := make([]*pglogrepl.TupleDataColumn, len(vals))
	for i, v := range vals {
		cols[i] = &pglogrepl.TupleDataColumn{DataType: pglogrepl.TupleDataTypeText, Data: []byte(v)}
	}
	return &pglogrepl.TupleData{ColumnNum: uint16(len(vals)), Columns: cols}
}

func accountsRel() *pglogrepl.RelationMessage {
	return &pglogrepl.RelationMessage{
		RelationID:   42,
		Namespace:    "public",
		RelationName: "accounts",
		Columns: []*pglogrepl.RelationMessageColumn{
			{Name: "id"},
			{Name: "email"},
		},
	}
}

func seedRelAndBegin(t *testing.T, d *decoder) {
	t.Helper()
	_, err := d.apply(accountsRel(), 0)
	require.NoError(t, err)
	_, err = d.apply(&pglogrepl.BeginMessage{FinalLSN: 0x10, Xid: 7}, 0)
	require.NoError(t, err)
}

func TestDecoderInsert(t *testing.T) {
	d := newDecoder()
	seedRelAndBegin(t, d)

	changes, err := d.apply(&pglogrepl.InsertMessage{RelationID: 42, Tuple: textTuple("1", "a@w.ai")}, 0x20)
	require.NoError(t, err)
	assert.Empty(t, changes, "rows are not visible before commit")
	changes, err = d.apply(&pglogrepl.CommitMessage{CommitLSN: 0x30}, 0)
	require.NoError(t, err)
	require.Len(t, changes, 1)

	c := changes[0]
	assert.Equal(t, OpInsert, c.Op)
	assert.Equal(t, "public", c.Schema)
	assert.Equal(t, "accounts", c.Table)
	assert.Equal(t, uint32(7), c.XID)
	assert.Equal(t, "0/20", c.LSN)
	assert.Equal(t, "0/30", c.CommitLSN)
	assert.Equal(t, map[string]any{"id": "1", "email": "a@w.ai"}, c.After)
	assert.Nil(t, c.Before)
}

func TestDecoderUpdate(t *testing.T) {
	d := newDecoder()
	seedRelAndBegin(t, d)

	changes, err := d.apply(&pglogrepl.UpdateMessage{
		RelationID: 42,
		OldTuple:   textTuple("1", "old@w.ai"),
		NewTuple:   textTuple("1", "new@w.ai"),
	}, 0x30)
	require.NoError(t, err)
	assert.Empty(t, changes, "rows are not visible before commit")
	changes, err = d.apply(&pglogrepl.CommitMessage{CommitLSN: 0x31}, 0)
	require.NoError(t, err)
	require.Len(t, changes, 1)

	c := changes[0]
	assert.Equal(t, OpUpdate, c.Op)
	assert.Equal(t, map[string]any{"id": "1", "email": "old@w.ai"}, c.Before)
	assert.Equal(t, map[string]any{"id": "1", "email": "new@w.ai"}, c.After)
}

func TestDecoderDelete(t *testing.T) {
	d := newDecoder()
	seedRelAndBegin(t, d)

	changes, err := d.apply(&pglogrepl.DeleteMessage{RelationID: 42, OldTuple: textTuple("1", "a@w.ai")}, 0x40)
	require.NoError(t, err)
	assert.Empty(t, changes, "rows are not visible before commit")
	changes, err = d.apply(&pglogrepl.CommitMessage{CommitLSN: 0x41}, 0)
	require.NoError(t, err)
	require.Len(t, changes, 1)

	c := changes[0]
	assert.Equal(t, OpDelete, c.Op)
	assert.Equal(t, map[string]any{"id": "1", "email": "a@w.ai"}, c.Before)
	assert.Nil(t, c.After)
}

func TestDecoderTruncate(t *testing.T) {
	d := newDecoder()
	seedRelAndBegin(t, d)

	changes, err := d.apply(&pglogrepl.TruncateMessage{RelationNum: 1, RelationIDs: []uint32{42}}, 0x50)
	require.NoError(t, err)
	assert.Empty(t, changes, "rows are not visible before commit")
	changes, err = d.apply(&pglogrepl.CommitMessage{CommitLSN: 0x51}, 0)
	require.NoError(t, err)
	require.Len(t, changes, 1)

	c := changes[0]
	assert.Equal(t, OpTruncate, c.Op)
	assert.Equal(t, "public", c.Schema)
	assert.Equal(t, "accounts", c.Table)
	assert.Nil(t, c.Before)
	assert.Nil(t, c.After)
}

func TestDecoderTruncateMultipleRelations(t *testing.T) {
	d := newDecoder()
	_, err := d.apply(accountsRel(), 0)
	require.NoError(t, err)
	_, err = d.apply(&pglogrepl.RelationMessage{
		RelationID: 43, Namespace: "public", RelationName: "orders",
		Columns: []*pglogrepl.RelationMessageColumn{{Name: "id"}},
	}, 0)
	require.NoError(t, err)
	_, err = d.apply(&pglogrepl.BeginMessage{FinalLSN: 0x10, Xid: 7}, 0)
	require.NoError(t, err)

	changes, err := d.apply(&pglogrepl.TruncateMessage{RelationNum: 2, RelationIDs: []uint32{42, 43}}, 0x60)
	require.NoError(t, err)
	assert.Empty(t, changes, "rows are not visible before commit")
	changes, err = d.apply(&pglogrepl.CommitMessage{CommitLSN: 0x61}, 0)
	require.NoError(t, err)
	require.Len(t, changes, 2)
	assert.Equal(t, "accounts", changes[0].Table)
	assert.Equal(t, "orders", changes[1].Table)
}

func TestDecoderTruncateUnknownRelation(t *testing.T) {
	d := newDecoder()
	_, err := d.apply(&pglogrepl.BeginMessage{FinalLSN: 0x10, Xid: 7}, 0)
	require.NoError(t, err)
	changes, err := d.apply(&pglogrepl.TruncateMessage{RelationNum: 1, RelationIDs: []uint32{999}}, 0x50)
	require.ErrorIs(t, err, ErrUnknownRelation)
	assert.Nil(t, changes)
}

func TestDecoderUnknownRelation(t *testing.T) {
	d := newDecoder()
	_, err := d.apply(&pglogrepl.BeginMessage{FinalLSN: 0x10, Xid: 7}, 0)
	require.NoError(t, err)
	changes, err := d.apply(&pglogrepl.InsertMessage{RelationID: 99, Tuple: textTuple("1")}, 0x10)
	require.ErrorIs(t, err, ErrUnknownRelation)
	assert.Nil(t, changes)
}

func TestDecoderRelationOnlyYieldsNothing(t *testing.T) {
	d := newDecoder()
	changes, err := d.apply(accountsRel(), 0)
	require.NoError(t, err)
	assert.Nil(t, changes)
}

func TestDecoderCommitClearsTransactionState(t *testing.T) {
	d := newDecoder()
	seedRelAndBegin(t, d)

	_, err := d.apply(&pglogrepl.CommitMessage{CommitLSN: 0x10}, 0)
	require.NoError(t, err)

	changes, err := d.apply(&pglogrepl.InsertMessage{RelationID: 42, Tuple: textTuple("9", "x@w.ai")}, 0x99)
	require.ErrorIs(t, err, ErrInvalidTransaction)
	assert.Nil(t, changes)
}

func TestDecoderSafeProgressOnlyAtTransactionBoundary(t *testing.T) {
	d := newDecoder()
	result, err := d.applyResult(accountsRel(), 0)
	require.NoError(t, err)
	assert.True(t, result.safe)

	result, err = d.applyResult(&pglogrepl.BeginMessage{FinalLSN: 0x10, Xid: 7}, 0)
	require.NoError(t, err)
	assert.False(t, result.safe)
	result, err = d.applyResult(&pglogrepl.InsertMessage{RelationID: 42, Tuple: textTuple("1", "a@w.ai")}, 0x20)
	require.NoError(t, err)
	assert.False(t, result.safe)
	result, err = d.applyResult(&pglogrepl.CommitMessage{CommitLSN: 0x30}, 0)
	require.NoError(t, err)
	assert.True(t, result.safe)
	assert.Len(t, result.changes, 1)
}

func TestDecoderAcceptsPgoutputMetadataMessages(t *testing.T) {
	d := newDecoder()
	metadata := []pglogrepl.Message{
		&pglogrepl.OriginMessage{},
		&pglogrepl.TypeMessage{},
		&pglogrepl.LogicalDecodingMessage{},
	}
	for _, msg := range metadata {
		result, err := d.applyResult(msg, 0)
		require.NoError(t, err)
		assert.True(t, result.safe)
	}

	_, err := d.applyResult(&pglogrepl.BeginMessage{FinalLSN: 0x10, Xid: 7}, 0)
	require.NoError(t, err)
	for _, msg := range metadata {
		result, err := d.applyResult(msg, 0)
		require.NoError(t, err)
		assert.False(t, result.safe, "metadata inside an open transaction cannot advance the checkpoint")
	}
	result, err := d.applyResult(&pglogrepl.CommitMessage{CommitLSN: 0x20}, 0)
	require.NoError(t, err)
	assert.True(t, result.safe)
}

func TestStreamingDecoderAcceptsPgoutputMetadataMessages(t *testing.T) {
	d := newStreamingDecoder()
	metadata := []pglogrepl.Message{
		&pglogrepl.TypeMessageV2{},
		&pglogrepl.LogicalDecodingMessageV2{},
	}
	for _, msg := range metadata {
		result, err := d.applyResult(msg, 0)
		require.NoError(t, err)
		assert.True(t, result.safe)
	}

	_, err := d.applyResult(&pglogrepl.StreamStartMessageV2{Xid: 7, FirstSegment: 1}, 0)
	require.NoError(t, err)
	for _, msg := range metadata {
		result, err := d.applyResult(msg, 0)
		require.NoError(t, err)
		assert.False(t, result.safe, "metadata inside a stream cannot advance the checkpoint")
	}
	_, err = d.applyResult(&pglogrepl.StreamStopMessageV2{}, 0)
	require.NoError(t, err)
	_, err = d.applyResult(&pglogrepl.StreamAbortMessageV2{Xid: 7, SubXid: 7}, 0)
	require.NoError(t, err)
}

func TestDecoderEnforcesTransactionChangeLimit(t *testing.T) {
	d := newDecoder(decoderLimits{maxChanges: 1, maxBytes: defaultMaxTransactionBytes})
	seedRelAndBegin(t, d)

	_, err := d.apply(&pglogrepl.InsertMessage{RelationID: 42, Tuple: textTuple("1", "a@w.ai")}, 0x20)
	require.NoError(t, err)
	_, err = d.apply(&pglogrepl.InsertMessage{RelationID: 42, Tuple: textTuple("2", "b@w.ai")}, 0x21)
	require.ErrorIs(t, err, ErrTransactionLimit)
}

func TestDecoderEnforcesTransactionByteLimitForStreamedSegments(t *testing.T) {
	d := newStreamingDecoder(decoderLimits{maxChanges: 100, maxBytes: 1})
	_, err := d.apply(relV2(), 0)
	require.NoError(t, err)
	_, err = d.apply(&pglogrepl.StreamStartMessageV2{Xid: 100, FirstSegment: 1}, 0)
	require.NoError(t, err)
	_, err = d.apply(insertV2(100, "1", "a@w.ai"), 0x20)
	require.ErrorIs(t, err, ErrTransactionLimit)
}

func TestTupleToMapNullAndToast(t *testing.T) {
	rel := &pglogrepl.RelationMessage{Columns: []*pglogrepl.RelationMessageColumn{
		{Name: "a"},
		{Name: "b"},
	}}
	tuple := &pglogrepl.TupleData{Columns: []*pglogrepl.TupleDataColumn{
		{DataType: pglogrepl.TupleDataTypeNull},
		{DataType: pglogrepl.TupleDataTypeToast},
	}}
	m := tupleToMap(rel, tuple)
	assert.Nil(t, m["a"])
	assert.Equal(t, unchangedTOAST, m["b"])
}

func TestTupleToMapNilGuards(t *testing.T) {
	assert.Nil(t, tupleToMap(nil, textTuple("x")))
	assert.Nil(t, tupleToMap(accountsRel(), nil))
}

func TestTupleToMapShortRelationDoesNotPanic(t *testing.T) {
	rel := &pglogrepl.RelationMessage{Columns: []*pglogrepl.RelationMessageColumn{{Name: "only"}}}
	m := tupleToMap(rel, textTuple("v1", "v2"))
	assert.Equal(t, map[string]any{"only": "v1"}, m)
}
