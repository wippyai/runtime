// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"testing"

	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/require"
)

func TestProgressUsesTransactionEndNotMessageSize(t *testing.T) {
	d := newDecoder()
	_, err := d.applyResult(&pglogrepl.BeginMessage{FinalLSN: 0x100, Xid: 1}, 0x10)
	require.NoError(t, err)
	result, err := d.applyResult(&pglogrepl.CommitMessage{CommitLSN: 0x100, TransactionEndLSN: 0x180}, 0x90)
	require.NoError(t, err)
	require.Equal(t, pglogrepl.LSN(0x180), result.position)
	metadata, err := d.applyResult(accountsRel(), 0x200)
	require.NoError(t, err)
	require.Zero(t, metadata.position, "metadata cannot invent an acknowledged transaction boundary")
}

func TestStreamProgressUsesTransactionEnd(t *testing.T) {
	d := newStreamingDecoder()
	_, err := d.applyResult(&pglogrepl.StreamStartMessageV2{Xid: 7, FirstSegment: 1}, 0)
	require.NoError(t, err)
	_, err = d.applyResult(&pglogrepl.StreamStopMessageV2{}, 0)
	require.NoError(t, err)
	result, err := d.applyResult(&pglogrepl.StreamCommitMessageV2{Xid: 7, CommitLSN: 0x100, TransactionEndLSN: 0x200}, 0x50)
	require.NoError(t, err)
	require.True(t, result.safe)
	require.Equal(t, pglogrepl.LSN(0x200), result.position)
}
