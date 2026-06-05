// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"testing"

	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func relV2() *pglogrepl.RelationMessageV2 {
	return &pglogrepl.RelationMessageV2{RelationMessage: *accountsRel()}
}

func insertV2(subxid uint32, id, email string) *pglogrepl.InsertMessageV2 {
	return &pglogrepl.InsertMessageV2{
		InsertMessage:            pglogrepl.InsertMessage{RelationID: 42, Tuple: textTuple(id, email)},
		InStreamMessageV2WithXid: pglogrepl.InStreamMessageV2WithXid{Xid: subxid},
	}
}

func TestStreamingDecoderBuffersUntilCommit(t *testing.T) {
	d := newStreamingDecoder()
	_, err := d.apply(relV2(), 0)
	require.NoError(t, err)
	_, err = d.apply(&pglogrepl.StreamStartMessageV2{Xid: 100, FirstSegment: 1}, 0)
	require.NoError(t, err)

	changes, err := d.apply(insertV2(100, "1", "a@w.ai"), 0x20)
	require.NoError(t, err)
	assert.Nil(t, changes, "in-stream change must be buffered, not emitted before commit")

	_, err = d.apply(&pglogrepl.StreamStopMessageV2{}, 0)
	require.NoError(t, err)

	changes, err = d.apply(&pglogrepl.StreamCommitMessageV2{Xid: 100, CommitLSN: 0x99}, 0)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, OpInsert, changes[0].Op)
	assert.Equal(t, "accounts", changes[0].Table)
	assert.Equal(t, uint32(100), changes[0].XID)
	assert.Equal(t, "0/99", changes[0].CommitLSN)
	assert.Equal(t, "a@w.ai", changes[0].After["email"])
}

func TestStreamingDecoderTopLevelAbortDiscards(t *testing.T) {
	d := newStreamingDecoder()
	_, _ = d.apply(relV2(), 0)
	_, _ = d.apply(&pglogrepl.StreamStartMessageV2{Xid: 100, FirstSegment: 1}, 0)
	_, _ = d.apply(insertV2(100, "1", "a@w.ai"), 0x20)

	_, err := d.apply(&pglogrepl.StreamAbortMessageV2{Xid: 100, SubXid: 100}, 0)
	require.NoError(t, err)

	changes := d.flushStream(100, 0x99)
	assert.Empty(t, changes, "aborted transaction must yield no changes")
}

func TestStreamingDecoderSubtransactionAbortDropsOnlyThatSubxid(t *testing.T) {
	d := newStreamingDecoder()
	_, _ = d.apply(relV2(), 0)
	_, _ = d.apply(&pglogrepl.StreamStartMessageV2{Xid: 100, FirstSegment: 1}, 0)
	_, _ = d.apply(insertV2(100, "1", "keep@w.ai"), 0x20)
	_, _ = d.apply(insertV2(200, "2", "drop@w.ai"), 0x30)

	_, err := d.apply(&pglogrepl.StreamAbortMessageV2{Xid: 100, SubXid: 200}, 0)
	require.NoError(t, err)

	changes, err := d.apply(&pglogrepl.StreamCommitMessageV2{Xid: 100, CommitLSN: 0x99}, 0)
	require.NoError(t, err)
	require.Len(t, changes, 1, "only the aborted subtransaction's change must be dropped")
	assert.Equal(t, "keep@w.ai", changes[0].After["email"])
}

func TestStreamingDecoderInterleavedTransactions(t *testing.T) {
	d := newStreamingDecoder()
	_, _ = d.apply(relV2(), 0)

	_, _ = d.apply(&pglogrepl.StreamStartMessageV2{Xid: 100, FirstSegment: 1}, 0)
	_, _ = d.apply(insertV2(100, "1", "tx100@w.ai"), 0x20)
	_, _ = d.apply(&pglogrepl.StreamStopMessageV2{}, 0)

	_, _ = d.apply(&pglogrepl.StreamStartMessageV2{Xid: 200, FirstSegment: 1}, 0)
	_, _ = d.apply(insertV2(200, "2", "tx200@w.ai"), 0x30)
	_, _ = d.apply(&pglogrepl.StreamStopMessageV2{}, 0)

	c200, err := d.apply(&pglogrepl.StreamCommitMessageV2{Xid: 200, CommitLSN: 0x40}, 0)
	require.NoError(t, err)
	require.Len(t, c200, 1)
	assert.Equal(t, "tx200@w.ai", c200[0].After["email"])

	c100, err := d.apply(&pglogrepl.StreamCommitMessageV2{Xid: 100, CommitLSN: 0x50}, 0)
	require.NoError(t, err)
	require.Len(t, c100, 1)
	assert.Equal(t, "tx100@w.ai", c100[0].After["email"])
}

func TestStreamingDecoderNonStreamedV2EmitsImmediately(t *testing.T) {
	d := newStreamingDecoder()
	_, _ = d.apply(relV2(), 0)
	_, _ = d.apply(&pglogrepl.BeginMessage{FinalLSN: 0x10, Xid: 7}, 0)

	changes, err := d.apply(insertV2(0, "1", "v2small@w.ai"), 0x20)
	require.NoError(t, err)
	require.Len(t, changes, 1, "non-streamed v2 insert (inStream=false) must emit immediately, not buffer")
	assert.Equal(t, uint32(7), changes[0].XID)
	assert.Equal(t, "v2small@w.ai", changes[0].After["email"])
}

func TestStreamingDecoderNonStreamedStillWorks(t *testing.T) {
	d := newStreamingDecoder()
	_, _ = d.apply(accountsRel(), 0)
	_, _ = d.apply(&pglogrepl.BeginMessage{FinalLSN: 0x10, Xid: 7}, 0)

	changes, err := d.apply(&pglogrepl.InsertMessage{RelationID: 42, Tuple: textTuple("1", "small@w.ai")}, 0x20)
	require.NoError(t, err)
	require.Len(t, changes, 1, "small (non-streamed) transactions must still emit immediately")
	assert.Equal(t, uint32(7), changes[0].XID)
}
