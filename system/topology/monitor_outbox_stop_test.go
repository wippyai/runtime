// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	sysrelay "github.com/wippyai/runtime/system/relay"
)

func TestMonitorOutboxStopPreservesUnfinishedObligations(t *testing.T) {
	target, caller := monitorOutboxPIDs()
	outbox, err := newMonitorOutbox(2, 4096)
	require.NoError(t, err)
	require.NoError(t, outbox.reserve(target, caller, "completed", 128))
	require.NoError(t, outbox.reserve(target, caller, "pending", 128))
	_, err = outbox.fill(target, caller, "completed", []byte("original terminal value"))
	require.NoError(t, err)
	first := outbox.stop()
	require.ErrorIs(t, first, ErrMonitorDeliveryIncomplete)
	require.Contains(t, first.Error(), "2 obligations (1 completed notices)")
	require.Same(t, first, outbox.stop(), "repeated stop must retain the unfinished outcome")
	require.ErrorIs(t, outbox.reserve(target, caller, "new", 128), context.Canceled)
	require.ErrorIs(t, outbox.replace(target, caller, "pending", "next", 128), context.Canceled)
	_, err = outbox.fill(target, caller, "pending", []byte("late"))
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, outbox.ack(target, caller, "completed"))
	require.False(t, outbox.release(target, caller, "pending"))
	value, ok := outbox.notice(target, caller, "completed")
	require.True(t, ok)
	require.Equal(t, []byte("original terminal value"), value)
	require.Len(t, outbox.records, 2)
}

func TestMonitorOutboxCleanStopSealsFutureAdmission(t *testing.T) {
	target, caller := monitorOutboxPIDs()
	outbox, err := newMonitorOutbox(1, 4096)
	require.NoError(t, err)
	require.NoError(t, outbox.stop())
	require.NoError(t, outbox.stop())
	require.ErrorIs(t, outbox.reserve(target, caller, "late", 128), context.Canceled)
}

func TestMonitorEndpointIncompleteStopStillReleasesOwnedRoute(t *testing.T) {
	node := sysrelay.NewNode("local")
	topo := NewTopology(monitorContextSender(func(context.Context, *relay.Package) error { return nil }), "local")
	stop, err := topo.StartRemoteMonitoring(context.Background(), node, MonitorConfig{MaxPending: 1, RequestTimeout: time.Second})
	require.NoError(t, err)
	outbox, err := newMonitorOutbox(1, 4096)
	require.NoError(t, err)
	target := pid.PID{Node: "local", Host: "process", UniqID: "actor"}
	caller := pid.PID{Node: "remote", Host: "registry"}
	require.NoError(t, outbox.reserve(target, caller, "pending", 128))
	// Fixture setup is synchronous; production must install before publication.
	topo.monitorEndpoint.terminalOutbox = outbox
	require.ErrorIs(t, stop(), ErrMonitorDeliveryIncomplete)
	_, exists := node.GetHost(monitorControlHostID)
	require.False(t, exists, "an incomplete-delivery error must not skip route teardown")
	release, err := node.RegisterOwnedHost(monitorControlHostID, monitorContextSender(func(context.Context, *relay.Package) error { return nil }))
	require.NoError(t, err)
	defer release()
	require.ErrorIs(t, stop(), ErrMonitorDeliveryIncomplete)
	_, exists = node.GetHost(monitorControlHostID)
	require.True(t, exists, "repeated error reporting must preserve replacement route")
}

func TestMonitorStopJoinsAcceptedCompletionBeforeSealingOutbox(t *testing.T) {
	target, caller := monitorOutboxPIDs()
	exchange, err := newMonitorExchange(context.Background(), target.Node, monitorContextSender(func(context.Context, *relay.Package) error { return nil }), 1, time.Second)
	require.NoError(t, err)
	exchange.terminalOutbox, err = newMonitorOutbox(1, 4096)
	require.NoError(t, err)
	require.NoError(t, exchange.terminalOutbox.reserve(target, caller, "accepted", 128))
	entered, finish := make(chan struct{}), make(chan struct{})
	completed := make(chan error, 1)
	go func() {
		completed <- exchange.withCompletion(context.Background(), func(context.Context) error {
			close(entered)
			<-finish
			_, err := exchange.terminalOutbox.fill(target, caller, "accepted", []byte("finished during join"))
			return err
		})
	}()
	<-entered
	stopped := make(chan error, 1)
	go func() { stopped <- exchange.stop() }()
	<-exchange.ctx.Done()
	close(finish)
	require.NoError(t, <-completed, "accepted completion must finish before outbox is sealed")
	require.ErrorIs(t, <-stopped, ErrMonitorDeliveryIncomplete)
	value, exists := exchange.terminalOutbox.notice(target, caller, "accepted")
	require.True(t, exists)
	require.Equal(t, []byte("finished during join"), value)
}
