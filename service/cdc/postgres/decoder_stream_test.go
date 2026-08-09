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

func TestStreamingDecoderRequiresStopBeforeCommitOrAbort(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  pglogrepl.Message
	}{
		{name: "commit", msg: &pglogrepl.StreamCommitMessageV2{Xid: 100, CommitLSN: 0x99}},
		{name: "abort", msg: &pglogrepl.StreamAbortMessageV2{Xid: 100, SubXid: 100}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newStreamingDecoder()
			_, err := d.apply(&pglogrepl.StreamStartMessageV2{Xid: 100, FirstSegment: 1}, 0)
			require.NoError(t, err)

			_, err = d.apply(tc.msg, 0)
			require.ErrorIs(t, err, ErrInvalidTransaction)
		})
	}
}

func TestStreamingDecoderTopLevelAbortDiscards(t *testing.T) {
	d := newStreamingDecoder()
	_, _ = d.apply(relV2(), 0)
	_, _ = d.apply(&pglogrepl.StreamStartMessageV2{Xid: 100, FirstSegment: 1}, 0)
	_, _ = d.apply(insertV2(100, "1", "a@w.ai"), 0x20)
	_, _ = d.apply(&pglogrepl.StreamStopMessageV2{}, 0)

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
	_, _ = d.apply(&pglogrepl.StreamStopMessageV2{}, 0)

	_, err := d.apply(&pglogrepl.StreamAbortMessageV2{Xid: 100, SubXid: 200}, 0)
	require.NoError(t, err)

	changes, err := d.apply(&pglogrepl.StreamCommitMessageV2{Xid: 100, CommitLSN: 0x99}, 0)
	require.NoError(t, err)
	require.Len(t, changes, 1, "only the aborted subtransaction's change must be dropped")
	assert.Equal(t, "keep@w.ai", changes[0].After["email"])
}

func TestStreamingDecoderSubtransactionAbortDropsNestedDescendants(t *testing.T) {
	d := newStreamingDecoder()
	_, _ = d.apply(relV2(), 0)
	_, _ = d.apply(&pglogrepl.StreamStartMessageV2{Xid: 100, FirstSegment: 1}, 0)
	_, _ = d.apply(insertV2(100, "1", "keep@w.ai"), 0x20)
	_, _ = d.apply(insertV2(200, "2", "drop-parent@w.ai"), 0x30)
	_, _ = d.apply(insertV2(300, "3", "drop-child@w.ai"), 0x40)
	_, _ = d.apply(&pglogrepl.StreamStopMessageV2{}, 0)

	// A subtransaction abort includes all of its nested subtransactions.
	// PostgreSQL represents this by truncating the streamed changes at the
	// aborted subtransaction's first offset, not by sending a parent XID on
	// every descendant row.
	_, err := d.apply(&pglogrepl.StreamAbortMessageV2{Xid: 100, SubXid: 200}, 0)
	require.NoError(t, err)

	changes, err := d.apply(&pglogrepl.StreamCommitMessageV2{Xid: 100, CommitLSN: 0x99}, 0)
	require.NoError(t, err)
	require.Len(t, changes, 1)
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

func TestStreamingDecoderNonStreamedV2EmitsAtCommit(t *testing.T) {
	d := newStreamingDecoder()
	_, _ = d.apply(relV2(), 0)
	_, _ = d.apply(&pglogrepl.BeginMessage{FinalLSN: 0x10, Xid: 7}, 0)

	changes, err := d.apply(insertV2(0, "1", "v2small@w.ai"), 0x20)
	require.NoError(t, err)
	assert.Empty(t, changes, "non-streamed v2 rows must wait for commit")
	changes, err = d.apply(&pglogrepl.CommitMessage{CommitLSN: 0x30}, 0)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, uint32(7), changes[0].XID)
	assert.Equal(t, "v2small@w.ai", changes[0].After["email"])
}

func TestStreamingDecoderNonStreamedStillWaitsForCommit(t *testing.T) {
	d := newStreamingDecoder()
	_, _ = d.apply(accountsRel(), 0)
	_, _ = d.apply(&pglogrepl.BeginMessage{FinalLSN: 0x10, Xid: 7}, 0)

	changes, err := d.apply(&pglogrepl.InsertMessage{RelationID: 42, Tuple: textTuple("1", "small@w.ai")}, 0x20)
	require.NoError(t, err)
	assert.Empty(t, changes, "non-streamed rows must wait for commit")
	changes, err = d.apply(&pglogrepl.CommitMessage{CommitLSN: 0x30}, 0)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, uint32(7), changes[0].XID)
}

func TestStreamingDecoderDoesNotMarkInterleavedCommitSafe(t *testing.T) {
	d := newStreamingDecoder()
	_, _ = d.apply(relV2(), 0)
	_, _ = d.apply(&pglogrepl.StreamStartMessageV2{Xid: 100, FirstSegment: 1}, 0)
	_, _ = d.apply(insertV2(100, "1", "tx100@w.ai"), 0x20)
	_, _ = d.apply(&pglogrepl.StreamStopMessageV2{}, 0)
	_, _ = d.apply(&pglogrepl.StreamStartMessageV2{Xid: 200, FirstSegment: 1}, 0)
	_, _ = d.apply(insertV2(200, "2", "tx200@w.ai"), 0x30)
	_, _ = d.apply(&pglogrepl.StreamStopMessageV2{}, 0)

	result, err := d.applyResult(&pglogrepl.StreamCommitMessageV2{Xid: 200, CommitLSN: 0x40}, 0)
	require.NoError(t, err)
	assert.False(t, result.safe, "an earlier streamed transaction is still open")
	assert.Len(t, result.changes, 1)

	result, err = d.applyResult(&pglogrepl.StreamCommitMessageV2{Xid: 100, CommitLSN: 0x50}, 0)
	require.NoError(t, err)
	assert.True(t, result.safe)
	assert.Len(t, result.changes, 1)
}

func TestStreamingDecoderAllowsOrdinaryTransactionAlongsideStream(t *testing.T) {
	d := newStreamingDecoder()
	_, _ = d.apply(relV2(), 0)
	_, _ = d.apply(&pglogrepl.StreamStartMessageV2{Xid: 100, FirstSegment: 1}, 0)
	_, _ = d.apply(insertV2(100, "1", "streamed@w.ai"), 0x20)
	_, _ = d.apply(&pglogrepl.StreamStopMessageV2{}, 0)

	_, err := d.apply(&pglogrepl.BeginMessage{FinalLSN: 0x30, Xid: 7}, 0)
	require.NoError(t, err)
	_, err = d.apply(insertV2(0, "2", "ordinary@w.ai"), 0x31)
	require.NoError(t, err)
	result, err := d.applyResult(&pglogrepl.CommitMessage{CommitLSN: 0x32}, 0)
	require.NoError(t, err)
	assert.False(t, result.safe, "streamed transaction is still buffered")
	require.Len(t, result.changes, 1)
	assert.Equal(t, "ordinary@w.ai", result.changes[0].After["email"])

	result, err = d.applyResult(&pglogrepl.StreamCommitMessageV2{Xid: 100, CommitLSN: 0x40}, 0)
	require.NoError(t, err)
	assert.True(t, result.safe)
	require.Len(t, result.changes, 1)
	assert.Equal(t, "streamed@w.ai", result.changes[0].After["email"])
}

func TestStreamingDecoderAcceptsMetadataMessages(t *testing.T) {
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

	_, err := d.applyResult(&pglogrepl.StreamStartMessageV2{Xid: 100, FirstSegment: 1}, 0)
	require.NoError(t, err)
	for _, msg := range metadata {
		result, err := d.applyResult(msg, 0)
		require.NoError(t, err)
		assert.False(t, result.safe)
	}
}
