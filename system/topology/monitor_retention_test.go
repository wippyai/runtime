// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
)

func TestMonitorRetentionAdmissionRefusalDoesNotInstallObserver(t *testing.T) {
	target := pid.PID{Node: "local", Host: "process", UniqID: "actor"}
	caller := pid.PID{Node: "remote", Host: "registry"}
	other := pid.PID{Node: "other", Host: "registry"}
	outbox, err := newMonitorOutbox(1, 4096)
	require.NoError(t, err)
	require.NoError(t, outbox.reserve(target, other, "occupied", 128))
	set, err := newRetainedRemoteMonitorSet(4, outbox, target, 128)
	require.NoError(t, err)
	require.ErrorIs(t, set.establish(caller, "", "first"), errRemoteMonitorCapacity)
	require.Empty(t, set.records, "failed reservation must not install an observer")
	require.True(t, outbox.release(target, other, "occupied"))
	require.NoError(t, set.establish(caller, "", "first"))
	require.NoError(t, set.establish(caller, "", "first"), "duplicate installation cannot need another slot")
	require.ErrorIs(t, set.establish(caller, "wrong", "next"), errRemoteMonitorConflict)
	require.NoError(t, set.release(caller, "first"))
	require.NoError(t, set.release(caller, "first"), "duplicate release preserves tombstone")
	require.ErrorIs(t, set.establish(caller, "", "first"), errRemoteMonitorConflict)
	require.NoError(t, set.establish(caller, "first", "next"))
	observers := set.close()
	require.Len(t, observers, 1)
	require.Equal(t, "next", observers[0].reference)
	require.ErrorIs(t, outbox.reserve(target, other, "blocked", 1), errMonitorOutboxCapacity, "completion must inherit its reservation")
	_, err = outbox.fill(target, caller, "next", []byte("terminal"))
	require.NoError(t, err)
	require.True(t, outbox.ack(target, caller, "next"))
	require.NoError(t, outbox.reserve(target, other, "free", 128))
}

func TestMonitorRetentionReplacementAtCapacityPreservesPredecessorOnRefusal(t *testing.T) {
	target := pid.PID{Node: "n", Host: "h", UniqID: "t"}
	caller := pid.PID{Node: "c", Host: "h"}
	// 5 PID bytes + 3 reference bytes + 4 reserved notice bytes.
	outbox, err := newMonitorOutbox(1, 12)
	require.NoError(t, err)
	set, err := newRetainedRemoteMonitorSet(1, outbox, target, 4)
	require.NoError(t, err)
	require.NoError(t, set.establish(caller, "", "one"))
	require.ErrorIs(t, set.establish(caller, "one", "too-long"), errRemoteMonitorCapacity)
	require.Equal(t, "one", set.records[caller].reference)
	require.NoError(t, set.establish(caller, "one", "two"), "same-size swap must work at full record/byte capacity")
	require.ErrorIs(t, set.release(caller, "one"), errRemoteMonitorConflict)
	require.False(t, outbox.ack(target, caller, "one"))
	observers := set.close()
	require.Len(t, observers, 1)
	require.Equal(t, "two", observers[0].reference)
	_, err = outbox.fill(target, caller, "two", []byte("done"))
	require.NoError(t, err)
	require.True(t, outbox.ack(target, caller, "two"))
}

func TestMonitorRetentionCloseRacesAdmissionWithoutLosingReservation(t *testing.T) {
	for i := 0; i < 32; i++ {
		target := pid.PID{Node: "local", Host: "process", UniqID: "actor"}
		caller := pid.PID{Node: "remote", Host: "registry"}
		outbox, err := newMonitorOutbox(1, 4096)
		require.NoError(t, err)
		set, err := newRetainedRemoteMonitorSet(1, outbox, target, 128)
		require.NoError(t, err)
		var admitted error
		var observers []remoteMonitorObserver
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); admitted = set.establish(caller, "", "ref") }()
		go func() { defer wg.Done(); observers = set.close() }()
		wg.Wait()
		if errors.Is(admitted, errRemoteMonitorClosed) {
			require.Empty(t, observers)
			require.False(t, outbox.release(target, caller, "ref"), "closed admission cannot leak a reservation")
		} else {
			require.NoError(t, admitted)
			require.Len(t, observers, 1)
			_, err := outbox.fill(target, caller, "ref", []byte("done"))
			require.NoError(t, err, "close must inherit every admitted obligation")
			require.True(t, outbox.ack(target, caller, "ref"))
		}
	}
}

func TestMonitorRetentionOversizedIdentityGetsCapacityRefusal(t *testing.T) {
	target := pid.PID{Node: "n", Host: "h", UniqID: "t"}
	caller := pid.PID{Node: "client-with-long-identity", Host: "h"}
	outbox, err := newMonitorOutbox(8, 12)
	require.NoError(t, err)
	set, err := newRetainedRemoteMonitorSet(8, outbox, target, 4)
	require.NoError(t, err)
	require.ErrorIs(t, set.establish(caller, "", "ref"), errRemoteMonitorCapacity, "valid over-budget control needs a classified refusal")
	require.Empty(t, set.close())
}
