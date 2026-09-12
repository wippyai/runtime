// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
)

func monitorOutboxPIDs() (pid.PID, pid.PID) {
	return pid.PID{Node: "target-node", Host: "target-host", UniqID: "target"},
		pid.PID{Node: "caller-node", Host: "caller-host", UniqID: "caller"}
}

func TestMonitorOutboxReserveCapacityAndDedupe(t *testing.T) {
	target, caller := monitorOutboxPIDs()
	outbox, err := newMonitorOutbox(2, 128)
	require.NoError(t, err)
	require.NoError(t, outbox.reserve(target, caller, "one", 6))
	require.NoError(t, outbox.reserve(target, caller, "one", 6), "same reservation must be idempotent")
	require.ErrorIs(t, outbox.reserve(target, caller, "one", 5), errMonitorOutboxConflict)
	require.NoError(t, outbox.reserve(target, caller, "two", 4))
	require.ErrorIs(t, outbox.reserve(target, caller, "three", 1), errMonitorOutboxCapacity)
	require.False(t, outbox.ack(target, caller, "stale"))
	_, err = outbox.fill(target, caller, "one", []byte("notice"))
	require.NoError(t, err)
	require.True(t, outbox.ack(target, caller, "one"))
	require.True(t, outbox.release(target, caller, "two"))
	require.NoError(t, outbox.reserve(target, caller, "three", 6))
}

func TestMonitorOutboxStaleReferenceCannotRemoveReplacement(t *testing.T) {
	target, caller := monitorOutboxPIDs()
	outbox, err := newMonitorOutbox(2, 256)
	require.NoError(t, err)
	require.NoError(t, outbox.reserve(target, caller, "old", 4))
	require.True(t, outbox.release(target, caller, "old"))
	require.NoError(t, outbox.reserve(target, caller, "new", 4))
	require.False(t, outbox.ack(target, caller, "old"))
	require.NoError(t, func() error {
		_, err := outbox.fill(target, caller, "new", []byte("new"))
		return err
	}())
	require.NotNil(t, func() []byte {
		value, ok := outbox.notice(target, caller, "new")
		if !ok {
			return nil
		}
		return value
	}(), "replacement remains after stale ACK")
	require.True(t, outbox.ack(target, caller, "new"))
	require.False(t, outbox.ack(target, caller, "new"))
}

func TestMonitorOutboxOversizePreservesReservationThenFills(t *testing.T) {
	target, caller := monitorOutboxPIDs()
	outbox, err := newMonitorOutbox(1, 128)
	require.NoError(t, err)
	require.NoError(t, outbox.reserve(target, caller, "one", 4))
	_, err = outbox.fill(target, caller, "one", []byte("12345"))
	require.ErrorIs(t, err, errMonitorOutboxOversize)
	_, ok := outbox.notice(target, caller, "one")
	require.False(t, ok, "oversize refusal leaves an empty reservation")
	require.ErrorIs(t, outbox.reserve(target, caller, "two", 1), errMonitorOutboxCapacity)
	stored, err := outbox.fill(target, caller, "one", []byte("1234"))
	require.NoError(t, err)
	require.Equal(t, []byte("1234"), stored)
	require.NoError(t, func() error {
		_, err := outbox.fill(target, caller, "one", []byte("1234"))
		return err
	}(), "same completion retry is idempotent")
	_, err = outbox.fill(target, caller, "one", []byte("different"))
	require.ErrorIs(t, err, errMonitorOutboxOversize, "oversize retry still cannot replace the notice")
}

func TestMonitorOutboxFillAndReadAreImmutable(t *testing.T) {
	target, caller := monitorOutboxPIDs()
	outbox, err := newMonitorOutbox(1, 128)
	require.NoError(t, err)
	require.NoError(t, outbox.reserve(target, caller, "one", 8))
	input := []byte("terminal")
	stored, err := outbox.fill(target, caller, "one", input)
	require.NoError(t, err)
	input[0] = 'X'
	stored[0] = 'Y'
	read, ok := outbox.notice(target, caller, "one")
	require.True(t, ok)
	require.Equal(t, []byte("terminal"), read)
	read[0] = 'Z'
	readAgain, ok := outbox.notice(target, caller, "one")
	require.True(t, ok)
	require.Equal(t, []byte("terminal"), readAgain)
}

func TestMonitorOutboxFilledNoticeCannotBeReplaced(t *testing.T) {
	target, caller := monitorOutboxPIDs()
	outbox, err := newMonitorOutbox(1, 128)
	require.NoError(t, err)
	require.NoError(t, outbox.reserve(target, caller, "one", 8))
	_, err = outbox.fill(target, caller, "one", []byte("original"))
	require.NoError(t, err)
	_, err = outbox.fill(target, caller, "one", []byte("changed"))
	require.ErrorIs(t, err, errMonitorOutboxConflict)
	stored, ok := outbox.notice(target, caller, "one")
	require.True(t, ok)
	require.Equal(t, []byte("original"), stored)
}

func TestMonitorOutboxConcurrentFinishReleaseAndAck(t *testing.T) {
	target, caller := monitorOutboxPIDs()
	outbox, err := newMonitorOutbox(1, 128)
	require.NoError(t, err)
	require.NoError(t, outbox.reserve(target, caller, "one", 32))

	var wg sync.WaitGroup
	var settled atomic.Int32
	for i := 0; i < 16; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); _, _ = outbox.fill(target, caller, "one", []byte("terminal")) }()
		go func() {
			defer wg.Done()
			if outbox.ack(target, caller, "one") {
				settled.Add(1)
			}
		}()
		go func() {
			defer wg.Done()
			if outbox.release(target, caller, "one") {
				settled.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), settled.Load(), "release/ACK race must settle exactly once")
	require.False(t, outbox.ack(target, caller, "one"))
	require.False(t, outbox.release(target, caller, "one"))
	require.NoError(t, outbox.reserve(target, caller, "new", 32), "settlement must return the capacity exactly once")
}

func TestMonitorOutboxRejectsInvalidReservation(t *testing.T) {
	target, caller := monitorOutboxPIDs()
	outbox, err := newMonitorOutbox(1, 128)
	require.NoError(t, err)
	for _, reference := range []string{"", string(make([]byte, 65))} {
		require.Error(t, outbox.reserve(target, caller, reference, 1))
	}
	_, err = outbox.fill(target, caller, "missing", []byte("x"))
	require.ErrorIs(t, err, errMonitorOutboxConflict)
	require.Error(t, outbox.reserve(target, caller, "one", 0))
	small, err := newMonitorOutbox(1, 63)
	require.NoError(t, err)
	require.Error(t, small.reserve(target, caller, "one", 5))
	require.Error(t, outbox.reserve(target, caller, "one", -1))
	var zero pid.PID
	require.Error(t, outbox.reserve(zero, caller, "one", 1))
	_, err = outbox.fill(target, caller, "one", nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, errMonitorOutboxConflict))
}

func TestMonitorOutboxAckCannotReleaseUnfilledReservation(t *testing.T) {
	target, caller := monitorOutboxPIDs()
	outbox, err := newMonitorOutbox(2, 256)
	require.NoError(t, err)
	require.NoError(t, outbox.reserve(target, caller, "one", 8))
	require.False(t, outbox.ack(target, caller, "one"))
	require.NoError(t, outbox.reserve(target, caller, "two", 8))
}

func TestMonitorOutboxReplaceSwapsAtFullCountAndBytes(t *testing.T) {
	target, caller := monitorOutboxPIDs()
	oldKey := monitorOutboxKeyOf(target, caller, "old")
	oldBytes, ok := monitorOutboxKeyBytes(oldKey)
	require.True(t, ok)
	outbox, err := newMonitorOutbox(1, oldBytes+8)
	require.NoError(t, err)
	require.NoError(t, outbox.reserve(target, caller, "old", 8))
	require.NoError(t, outbox.replace(target, caller, "old", "new", 8))
	_, filled := outbox.notice(target, caller, "old")
	require.False(t, filled)
	require.False(t, outbox.ack(target, caller, "old"))
	_, err = outbox.fill(target, caller, "new", []byte("done"))
	require.NoError(t, err)
	require.True(t, outbox.ack(target, caller, "new"))
}

func TestMonitorOutboxReplaceRefusalPreservesPredecessor(t *testing.T) {
	target, caller := monitorOutboxPIDs()
	oldKey := monitorOutboxKeyOf(target, caller, "old")
	oldBytes, ok := monitorOutboxKeyBytes(oldKey)
	require.True(t, ok)
	outbox, err := newMonitorOutbox(1, oldBytes+8)
	require.NoError(t, err)
	require.NoError(t, outbox.reserve(target, caller, "old", 8))
	longReference := string(make([]byte, monitorOutboxMaxRefBytes))
	require.ErrorIs(t, outbox.replace(target, caller, "old", longReference, 8), errMonitorOutboxCapacity)
	_, err = outbox.fill(target, caller, "old", []byte("old"))
	require.NoError(t, err)
	require.ErrorIs(t, outbox.replace(target, caller, "old", "new", 8), errMonitorOutboxConflict)
	_, filled := outbox.notice(target, caller, "old")
	require.True(t, filled)
}

func TestMonitorOutboxReplaceRejectsWrongPreviousAndDuplicateNext(t *testing.T) {
	target, caller := monitorOutboxPIDs()
	outbox, err := newMonitorOutbox(2, 256)
	require.NoError(t, err)
	require.NoError(t, outbox.reserve(target, caller, "old", 8))
	require.ErrorIs(t, outbox.replace(target, caller, "wrong", "new", 8), errMonitorOutboxConflict)
	require.NoError(t, outbox.reserve(target, caller, "new", 8))
	require.ErrorIs(t, outbox.replace(target, caller, "old", "new", 8), errMonitorOutboxConflict)
	_, err = outbox.fill(target, caller, "old", []byte("old"))
	require.NoError(t, err)
	require.False(t, outbox.ack(target, caller, "new"), "unfilled duplicate successor remains reserved")
}

func TestMonitorOutboxIdentityAndNoticeShareByteBudget(t *testing.T) {
	target := pid.PID{Node: "n", Host: "h", UniqID: "t"}
	caller := pid.PID{Node: "c", Host: "h"}
	// Six identity/reference bytes plus four reserved notice bytes.
	outbox, err := newMonitorOutbox(8, 10)
	require.NoError(t, err)
	require.NoError(t, outbox.reserve(target, caller, "r", 4))
	require.ErrorIs(t, outbox.reserve(target, caller, "s", 1), errMonitorOutboxCapacity, "byte capacity must bind even with spare record slots")
	require.True(t, outbox.release(target, caller, "r"))
	require.NoError(t, outbox.reserve(target, caller, "s", 4))
}
