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
	require.Len(t, changes, 1)

	c := changes[0]
	assert.Equal(t, OpInsert, c.Op)
	assert.Equal(t, "public", c.Schema)
	assert.Equal(t, "accounts", c.Table)
	assert.Equal(t, uint32(7), c.XID)
	assert.Equal(t, "0/20", c.LSN)
	assert.Equal(t, "0/10", c.CommitLSN)
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

	changes, err := d.apply(&pglogrepl.TruncateMessage{RelationNum: 2, RelationIDs: []uint32{42, 43}}, 0x60)
	require.NoError(t, err)
	require.Len(t, changes, 2)
	assert.Equal(t, "accounts", changes[0].Table)
	assert.Equal(t, "orders", changes[1].Table)
}

func TestDecoderTruncateUnknownRelation(t *testing.T) {
	d := newDecoder()
	changes, err := d.apply(&pglogrepl.TruncateMessage{RelationNum: 1, RelationIDs: []uint32{999}}, 0x50)
	require.ErrorIs(t, err, ErrUnknownRelation)
	assert.Nil(t, changes)
}

func TestDecoderUnknownRelation(t *testing.T) {
	d := newDecoder()
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
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, uint32(0), changes[0].XID, "xid must not leak from a committed transaction")
	assert.Equal(t, "0/0", changes[0].CommitLSN)
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
