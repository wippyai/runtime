// SPDX-License-Identifier: MPL-2.0

package remote

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	relaysys "github.com/wippyai/runtime/system/relay"
	topologysys "github.com/wippyai/runtime/system/topology"
)

func inboundRequest(t *testing.T, in *Inbound, c Control, connection <-chan struct{}) (Control, error) {
	t.Helper()
	pkg, err := EncodeControl(c)
	require.NoError(t, err)
	defer relay.ReleasePackage(pkg)
	pkg.Ingress = relay.IngressIdentity{Node: c.Watcher.Node, Authenticated: true, IntegrityProtected: true, ConnectionClosed: connection}
	return in.Apply(pkg)
}

func inboundFixture(t *testing.T, receipts int, install InstallTarget) (*Authority, *Inbound, Control, chan struct{}) {
	t.Helper()
	c := testControl()
	authority, err := NewAuthority(c.Target.Node, 4)
	require.NoError(t, err)
	in, err := NewInbound(authority, install, receipts)
	require.NoError(t, err)
	t.Cleanup(func() { in.Close(); authority.Close() })
	token, err := authority.Grant(GrantSpec{PeerNode: c.Watcher.Node, Watcher: c.Watcher, Target: c.Target, Expires: time.Now().Add(time.Minute)})
	require.NoError(t, err)
	c.Grant = token.String()
	return authority, in, c, make(chan struct{})
}

func TestInboundReceiptsAndReleaseReservation(t *testing.T) {
	var installs, releases atomic.Int64
	_, in, c, connection := inboundFixture(t, 2, func(pid.PID) (func(), error) { installs.Add(1); return func() { releases.Add(1) }, nil })
	reply, err := inboundRequest(t, in, c, connection)
	require.NoError(t, err)
	require.Equal(t, InstalledControl, reply.Kind)
	replay, err := inboundRequest(t, in, c, connection)
	require.NoError(t, err)
	require.Equal(t, reply, replay)
	require.EqualValues(t, 1, installs.Load())
	conflict := c
	conflict.Kind = ReleaseControl
	_, err = inboundRequest(t, in, conflict, connection)
	require.ErrorIs(t, err, ErrControlConflict)
	require.Zero(t, releases.Load())
	extra := c
	extra.RequestID = strings.Repeat("c", 32)
	reply, err = inboundRequest(t, in, extra, connection)
	require.NoError(t, err)
	require.Equal(t, RejectedControl, reply.Kind)
	require.EqualValues(t, 1, installs.Load())
	// Admission reserved capacity for release, even though another monitor
	// request was refused at the receipt bound.
	extra.Kind = ReleaseControl
	reply, err = inboundRequest(t, in, extra, connection)
	require.NoError(t, err)
	require.Equal(t, ReleasedControl, reply.Kind)
	_, err = inboundRequest(t, in, extra, connection)
	require.NoError(t, err)
	require.EqualValues(t, 1, releases.Load())
}

func TestInboundGrantRevocationCleansAndReclaims(t *testing.T) {
	var releases atomic.Int64
	authority, in, c, connection := inboundFixture(t, 2, func(pid.PID) (func(), error) { return func() { releases.Add(1) }, nil })
	_, err := inboundRequest(t, in, c, connection)
	require.NoError(t, err)
	token, err := ParseToken(c.Grant)
	require.NoError(t, err)
	require.True(t, authority.Revoke(token))
	require.Eventually(t, func() bool { return releases.Load() == 1 }, time.Second, time.Millisecond)
	_, err = inboundRequest(t, in, c, connection)
	require.Error(t, err)
	replacement, err := authority.Grant(GrantSpec{PeerNode: c.Watcher.Node, Watcher: c.Watcher, Target: c.Target, Expires: time.Now().Add(time.Minute)})
	require.NoError(t, err)
	c.Grant = replacement.String()
	_, err = inboundRequest(t, in, c, connection)
	require.NoError(t, err)
	close(connection)
	require.Eventually(t, func() bool { return releases.Load() == 2 }, time.Second, time.Millisecond)
}

func TestInboundMissingTargetAndPartialCleanup(t *testing.T) {
	var installs, releases atomic.Int64
	_, in, c, connection := inboundFixture(t, 2, func(pid.PID) (func(), error) {
		installs.Add(1)
		return func() { releases.Add(1) }, topology.ErrPIDNotRegistered
	})
	response, err := inboundRequest(t, in, c, connection)
	require.NoError(t, err)
	require.Equal(t, MissingControl, response.Kind)
	_, err = inboundRequest(t, in, c, connection)
	require.NoError(t, err)
	require.EqualValues(t, 1, installs.Load())
	require.EqualValues(t, 1, releases.Load())
}

type localExitReceiver struct{ events chan *topology.ExitEvent }

func (r localExitReceiver) Send(pkg *relay.Package) error {
	event := pkg.Messages[0].Payloads[0].Data().(*topology.ExitEvent)
	r.events <- event
	relay.ReleasePackage(pkg)
	return nil
}

func TestInboundUsesActualLocalTopologyCompletion(t *testing.T) {
	target := testControl().Target
	node := relaysys.NewNode(target.Node)
	topo := topologysys.NewTopology(node, target.Node)
	observer := pid.PID{Node: target.Node, Host: HostID}
	events := make(chan *topology.ExitEvent, 1)
	require.NoError(t, node.RegisterHost(HostID, localExitReceiver{events}))
	require.NoError(t, topo.Register(observer))
	require.NoError(t, topo.Register(target))
	_, in, c, connection := inboundFixture(t, 8, func(target pid.PID) (func(), error) {
		if err := topo.Monitor(observer, target); err != nil {
			return nil, err
		}
		return func() { _ = topo.Demonitor(observer, target) }, nil
	})
	response, err := inboundRequest(t, in, c, connection)
	require.NoError(t, err)
	require.Equal(t, InstalledControl, response.Kind)
	topo.Complete(target, &runtime.Result{Value: payload.NewPayload("finished", payload.String)})
	event := <-events
	require.Equal(t, topology.Exit, event.Kind)
	require.True(t, samePID(target, event.From))
	exits, err := in.TargetExited(event.From, event.Result)
	require.NoError(t, err)
	require.Len(t, exits, 1)
	require.Equal(t, c.Grant, exits[0].Monitor.Grant)
	require.Equal(t, "finished", exits[0].Result.Value.Data())
	repeat, err := in.TargetExited(event.From, event.Result)
	require.NoError(t, err)
	require.Empty(t, repeat)
	c.RequestID = strings.Repeat("d", 32)
	response, err = inboundRequest(t, in, c, connection)
	require.NoError(t, err)
	require.Equal(t, RejectedControl, response.Kind)
}

func TestInboundCloseFencesFurtherAdmission(t *testing.T) {
	var releases atomic.Int64
	_, in, c, connection := inboundFixture(t, 4, func(pid.PID) (func(), error) { return func() { releases.Add(1) }, nil })
	_, err := inboundRequest(t, in, c, connection)
	require.NoError(t, err)
	in.Close()
	in.Close()
	require.EqualValues(t, 1, releases.Load())
	_, err = inboundRequest(t, in, c, connection)
	require.ErrorIs(t, err, ErrInboundClosed)
}

func TestInboundInstallationKeepsOneRequestIdentity(t *testing.T) {
	_, in, original, connection := inboundFixture(t, 8, func(pid.PID) (func(), error) { return func() {}, nil })
	reply, err := inboundRequest(t, in, original, connection)
	require.NoError(t, err)
	require.Equal(t, InstalledControl, reply.Kind)
	other := original
	other.RequestID = strings.Repeat("e", 32)
	reply, err = inboundRequest(t, in, other, connection)
	require.NoError(t, err)
	require.Equal(t, RejectedControl, reply.Kind)
	exits, err := in.TargetExited(original.Target, &runtime.Result{Value: payload.NewPayload("done", payload.String)})
	require.NoError(t, err)
	require.Len(t, exits, 1)
	require.Equal(t, original.RequestID, exits[0].Monitor.RequestID)
}

func TestInboundSharedTargetObservation(t *testing.T) {
	for _, releaseFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(releaseFirst), func(t *testing.T) {
			target := testControl().Target
			node := relaysys.NewNode(target.Node)
			topo := topologysys.NewTopology(node, target.Node)
			observer := pid.PID{Node: target.Node, Host: HostID}
			events := make(chan *topology.ExitEvent, 1)
			require.NoError(t, node.RegisterHost(HostID, localExitReceiver{events}))
			require.NoError(t, topo.Register(observer))
			require.NoError(t, topo.Register(target))
			var installs, releases atomic.Int64
			authority, in, first, connection := inboundFixture(t, 4, func(target pid.PID) (func(), error) {
				installs.Add(1)
				if err := topo.Monitor(observer, target); err != nil {
					return nil, err
				}
				return func() { releases.Add(1); _ = topo.Demonitor(observer, target) }, nil
			})
			reply, err := inboundRequest(t, in, first, connection)
			require.NoError(t, err)
			require.Equal(t, InstalledControl, reply.Kind)
			// Rejected identities must not consume another relationship's capacity.
			for n := range 128 {
				flood := first
				flood.RequestID = fmt.Sprintf("%032x", n)
				if flood.RequestID == first.RequestID {
					continue
				}
				reply, err = inboundRequest(t, in, flood, connection)
				require.NoError(t, err)
				require.Equal(t, RejectedControl, reply.Kind)
			}
			second := first
			second.Watcher.UniqID = "second-watcher"
			second.RequestID = strings.Repeat("f", 32)
			token, err := authority.Grant(GrantSpec{PeerNode: second.Watcher.Node, Watcher: second.Watcher, Target: target, Expires: time.Now().Add(time.Minute)})
			require.NoError(t, err)
			second.Grant = token.String()
			reply, err = inboundRequest(t, in, second, connection)
			require.NoError(t, err)
			require.Equal(t, InstalledControl, reply.Kind)
			require.EqualValues(t, 1, installs.Load())
			if releaseFirst {
				release := first
				release.Kind = ReleaseControl
				release.RequestID = strings.Repeat("d", 32)
				reply, err = inboundRequest(t, in, release, connection)
				require.NoError(t, err)
				require.Equal(t, ReleasedControl, reply.Kind)
				require.Zero(t, releases.Load())
			}
			topo.Complete(target, &runtime.Result{Value: payload.NewPayload("shared-result", payload.String)})
			var event *topology.ExitEvent
			select {
			case event = <-events:
			case <-time.After(time.Second):
				t.Fatal("remaining observer lost completion")
			}
			exits, err := in.TargetExited(event.From, event.Result)
			require.NoError(t, err)
			expected := []string{second.RequestID}
			if !releaseFirst {
				expected = append(expected, first.RequestID)
			}
			actual := make([]string, 0, len(exits))
			for _, exit := range exits {
				actual = append(actual, exit.Monitor.RequestID)
				require.Equal(t, "shared-result", exit.Result.Value.Data())
			}
			require.ElementsMatch(t, expected, actual)
			require.EqualValues(t, 1, releases.Load())
		})
	}
}

func TestInboundRetirementWinsDelayedLocalCompletion(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		t.Run(fmt.Sprint(disconnect), func(t *testing.T) {
			var releases atomic.Int64
			authority, in, c, connection := inboundFixture(t, 2, func(pid.PID) (func(), error) {
				return func() { releases.Add(1) }, nil
			})
			_, err := inboundRequest(t, in, c, connection)
			require.NoError(t, err)
			if disconnect {
				close(connection)
			} else {
				token, parseErr := ParseToken(c.Grant)
				require.NoError(t, parseErr)
				require.True(t, authority.Revoke(token))
			}
			exits, err := in.TargetExited(c.Target, &runtime.Result{Value: payload.NewPayload("late", payload.String)})
			require.NoError(t, err)
			require.Empty(t, exits)
			require.Eventually(t, func() bool { return releases.Load() == 1 }, time.Second, time.Millisecond)
		})
	}
}
