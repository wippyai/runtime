// SPDX-License-Identifier: MPL-2.0

package remote

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/relay"
	relaysys "github.com/wippyai/runtime/system/relay"
)

type receiverRetention struct{ released atomic.Int64 }

func (r *receiverRetention) Release() { r.released.Add(1) }

func TestOriginReceiverRoutesVerifiedCompletionAndOwnsOnlyAcceptedPackages(t *testing.T) {
	c := completionFixture(t)
	ingress := relay.IngressIdentity{Node: c.Monitor.Target.Node, Authenticated: true, IntegrityProtected: true, ConnectionClosed: make(chan struct{})}
	delivered := make(chan Completion, 1)
	out, err := NewOutbound(c.Monitor.Watcher.Node, 1, func(c Completion) error { delivered <- c; return nil })
	require.NoError(t, err)
	require.NoError(t, out.Track(c.Monitor, ingress, time.Now().Add(time.Minute)))
	receiver, err := NewOriginReceiver(out)
	require.NoError(t, err)
	defer receiver.Close()
	node := relaysys.NewNode(c.Monitor.Watcher.Node)
	require.NoError(t, node.RegisterHost(HostID, receiver))
	// Native host routing delivers to the receiver, not an actor mailbox.
	receipt := c.Monitor
	receipt.Kind = InstalledControl
	pkg, err := EncodeControl(receipt)
	require.NoError(t, err)
	pkg.Ingress = ingress
	accepted := &receiverRetention{}
	pkg.Messages[0].SetRetentionLease(accepted)
	require.NoError(t, node.Send(pkg))
	require.EqualValues(t, 1, accepted.released.Load())
	pkg, err = EncodeCompletion(c)
	require.NoError(t, err)
	pkg.Ingress = ingress
	completionLease := &receiverRetention{}
	pkg.Messages[0].SetRetentionLease(completionLease)
	require.NoError(t, node.Send(pkg))
	require.EqualValues(t, 1, completionLease.released.Load())
	select {
	case got := <-delivered:
		require.Equal(t, c.Result, got.Result)
	default:
		t.Fatal("verified completion not delivered")
	}
	receiver.Close()
	pkg, err = EncodeCompletion(c)
	require.NoError(t, err)
	pkg.Ingress = ingress
	rejected := &receiverRetention{}
	pkg.Messages[0].SetRetentionLease(rejected)
	require.ErrorIs(t, node.Send(pkg), ErrMonitorExpectation)
	require.Zero(t, rejected.released.Load())
	relay.ReleasePackage(pkg)
	require.EqualValues(t, 1, rejected.released.Load())
	select {
	case <-delivered:
		t.Fatal("completion delivered after receiver closed")
	default:
	}
}
