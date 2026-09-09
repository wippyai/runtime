// SPDX-License-Identifier: MPL-2.0

package remote

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/relay"
)

func outboundPacket(t *testing.T, c Completion, ingress relay.IngressIdentity, exit bool) *relay.Package {
	t.Helper()
	var pkg *relay.Package
	var err error
	if exit {
		pkg, err = EncodeCompletion(c)
	} else {
		receipt := c.Monitor
		receipt.Kind = InstalledControl
		pkg, err = EncodeControl(receipt)
	}
	require.NoError(t, err)
	pkg.Ingress = ingress
	t.Cleanup(func() { relay.ReleasePackage(pkg) })
	return pkg
}

func TestOutboundCompletionRequiresInstalledExactRelationship(t *testing.T) {
	for _, early := range []bool{false, true} {
		t.Run(map[bool]string{false: "receipt_first", true: "exit_first"}[early], func(t *testing.T) {
			c := completionFixture(t)
			connection := make(chan struct{})
			ingress := relay.IngressIdentity{Node: c.Monitor.Target.Node, Authenticated: true, IntegrityProtected: true, ConnectionClosed: connection}
			var delivered []Completion
			out, err := NewOutbound(c.Monitor.Watcher.Node, 2, func(c Completion) error { delivered = append(delivered, c); return nil })
			require.NoError(t, err)
			t.Cleanup(out.Close)
			expiry := time.Now().Add(time.Minute)
			require.NoError(t, out.Track(c.Monitor, ingress, expiry))
			first := outboundPacket(t, c, ingress, early)
			second := outboundPacket(t, c, ingress, !early)
			require.NoError(t, out.Apply(first))
			require.Empty(t, delivered)
			require.NoError(t, out.Apply(second))
			require.Len(t, delivered, 1)
			require.Equal(t, c.Result, delivered[0].Result)
			require.NoError(t, out.Apply(first))
			require.NoError(t, out.Apply(second))
			require.Len(t, delivered, 1)
			require.NoError(t, out.Track(c.Monitor, ingress, expiry))
			changed := c
			changed.Result.Data = []byte("different")
			require.ErrorIs(t, out.Apply(outboundPacket(t, changed, ingress, true)), ErrControlConflict)
			wrong := c
			wrong.Monitor.RequestID = "cccccccccccccccccccccccccccccccc"
			require.ErrorIs(t, out.Apply(outboundPacket(t, wrong, ingress, true)), ErrMonitorExpectation)
			reconnected := ingress
			reconnected.ConnectionClosed = make(chan struct{})
			require.ErrorIs(t, out.Apply(outboundPacket(t, c, reconnected, true)), ErrMonitorExpectation)
		})
	}
}

func TestOutboundRetirementAndQueueRejection(t *testing.T) {
	c := completionFixture(t)
	ingress := relay.IngressIdentity{Node: c.Monitor.Target.Node, Authenticated: true, IntegrityProtected: true, ConnectionClosed: make(chan struct{})}
	attempts := 0
	queueFull := errors.New("queue full")
	out, err := NewOutbound(c.Monitor.Watcher.Node, 1, func(Completion) error {
		attempts++
		if attempts == 1 {
			return queueFull
		}
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, out.Track(c.Monitor, ingress, time.Now().Add(time.Minute)))
	require.NoError(t, out.Apply(outboundPacket(t, c, ingress, false)))
	require.ErrorIs(t, out.Apply(outboundPacket(t, c, ingress, true)), queueFull)
	require.NoError(t, out.Apply(outboundPacket(t, c, ingress, true)))
	require.Equal(t, 2, attempts)
	token, err := ParseToken(c.Monitor.Grant)
	require.NoError(t, err)
	out.Cancel(token)
	require.ErrorIs(t, out.Track(c.Monitor, ingress, time.Now().Add(time.Minute)), ErrMonitorExpectation)
	require.ErrorIs(t, out.Apply(outboundPacket(t, c, ingress, true)), ErrMonitorExpectation)
	require.Equal(t, 2, attempts)
	out.Close()
	require.ErrorIs(t, out.Track(c.Monitor, ingress, time.Now().Add(time.Minute)), ErrMonitorExpectation)
}

func TestOutboundDisconnectDoesNotDeliverBufferedCompletion(t *testing.T) {
	c := completionFixture(t)
	connection := make(chan struct{})
	ingress := relay.IngressIdentity{Node: c.Monitor.Target.Node, Authenticated: true, IntegrityProtected: true, ConnectionClosed: connection}
	calls := 0
	out, err := NewOutbound(c.Monitor.Watcher.Node, 1, func(Completion) error { calls++; return nil })
	require.NoError(t, err)
	defer out.Close()
	require.NoError(t, out.Track(c.Monitor, ingress, time.Now().Add(time.Minute)))
	require.NoError(t, out.Apply(outboundPacket(t, c, ingress, true)))
	close(connection)
	require.Error(t, out.Apply(outboundPacket(t, c, ingress, false)))
	require.Zero(t, calls)
	other := c.Monitor
	other.Grant = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	ingress.ConnectionClosed = make(chan struct{})
	require.NoError(t, out.Track(other, ingress, time.Now().Add(time.Minute)))
}

func TestOutboundCancelJoinsAdmittedDelivery(t *testing.T) {
	c := completionFixture(t)
	ingress := relay.IngressIdentity{Node: c.Monitor.Target.Node, Authenticated: true, IntegrityProtected: true, ConnectionClosed: make(chan struct{})}
	entered := make(chan struct{})
	release := make(chan struct{})
	out, err := NewOutbound(c.Monitor.Watcher.Node, 1, func(Completion) error {
		close(entered)
		<-release // Hold the test callback to expose the cancellation gate.
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, out.Track(c.Monitor, ingress, time.Now().Add(time.Minute)))
	require.NoError(t, out.Apply(outboundPacket(t, c, ingress, false)))
	pkg := outboundPacket(t, c, ingress, true)
	delivered := make(chan error, 1)
	go func() { delivered <- out.Apply(pkg) }()
	<-entered
	token, err := ParseToken(c.Monitor.Grant)
	require.NoError(t, err)
	cancelled := make(chan struct{})
	go func() { out.Cancel(token); close(cancelled) }()
	select {
	case <-cancelled:
		close(release)
		t.Fatal("Cancel returned while admitted delivery was still running")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-delivered)
	<-cancelled
	require.ErrorIs(t, out.Apply(pkg), ErrMonitorExpectation)
	out.Close()
}

func TestOutboundCancelTombstonePreservesReplayFenceAndCapacity(t *testing.T) {
	c := completionFixture(t)
	connection := make(chan struct{})
	ingress := relay.IngressIdentity{Node: c.Monitor.Target.Node, Authenticated: true, IntegrityProtected: true, ConnectionClosed: connection}
	calls := 0
	out, err := NewOutbound(c.Monitor.Watcher.Node, 1, func(Completion) error { calls++; return nil })
	require.NoError(t, err)
	defer out.Close()
	expiry := time.Now().Add(time.Minute)
	require.NoError(t, out.Track(c.Monitor, ingress, expiry))
	token, err := ParseToken(c.Monitor.Grant)
	require.NoError(t, err)
	out.Cancel(token)
	require.ErrorIs(t, out.Track(c.Monitor, ingress, expiry), ErrMonitorExpectation)
	require.ErrorIs(t, out.Apply(outboundPacket(t, c, ingress, false)), ErrMonitorExpectation)
	require.ErrorIs(t, out.Apply(outboundPacket(t, c, ingress, true)), ErrMonitorExpectation)
	require.Zero(t, calls)
	replacement := c.Monitor
	replacement.Grant = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	require.ErrorIs(t, out.Track(replacement, ingress, expiry), ErrGrantCapacity)
	close(connection)
	ingress.ConnectionClosed = make(chan struct{})
	require.NoError(t, out.Track(replacement, ingress, expiry))
}
