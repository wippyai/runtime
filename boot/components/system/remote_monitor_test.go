// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apipayload "github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	relayapi "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	topapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/cluster/internode"
	"github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/topology"
)

// wireNode takes the same encode/decode and local delivery path as internode.
type wireNode struct {
	codec *internode.MessageCodec
	node  *relay.Node
	topo  *topology.Topology
}

type countReceiver struct{ sends atomic.Int32 }

func (r *countReceiver) Send(pkg *relayapi.Package) error {
	r.sends.Add(1)
	relayapi.ReleasePackage(pkg)
	return nil
}

func (w *wireNode) Send(pkg *relayapi.Package) error {
	data, err := w.codec.Encode(pkg)
	if err != nil {
		return err
	}
	relayapi.ReleasePackage(pkg)
	decoded, err := w.codec.Decode(data)
	if err != nil {
		return err
	}
	if err := deliverInternodePackage(w.node, w.topo, decoded); err != nil {
		relayapi.ReleasePackage(decoded)
		return err
	}
	return nil
}

func TestRemoteProcessExitReachesWatcher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	codec := internode.NewMessageCodec(payload.NewTranscoder())
	a := relay.NewNode("node-a")
	b := relay.NewNode("node-b")
	aBox := relay.NewMailbox(ctx, relay.WithBufferSize(4))
	bBox := relay.NewMailbox(ctx, relay.WithBufferSize(4))
	require.NoError(t, a.RegisterHost("watcher", aBox))
	require.NoError(t, b.RegisterHost("worker", bBox))
	aToB := &wireNode{codec: codec, node: b}
	bToA := &wireNode{codec: codec, node: a}
	aRouter := relay.NewRouter(a, aToB)
	bRouter := relay.NewRouter(b, bToA)
	aTopo := topology.NewTopology(aRouter, a.ID())
	bTopo := topology.NewTopology(bRouter, b.ID())
	aToB.topo = bTopo
	bToA.topo = aTopo
	watcher := pid.PID{Node: a.ID(), Host: "watcher", UniqID: "watcher"}
	target := pid.PID{Node: b.ID(), Host: "worker", UniqID: "target"}
	watcher = watcher.Precomputed()
	target = target.Precomputed()
	require.NoError(t, aTopo.Register(watcher))
	require.NoError(t, bTopo.Register(target))
	exits := make(chan *relayapi.Package, 2)
	detach, err := a.Attach(watcher, exits)
	require.NoError(t, err)
	defer detach()
	// The worker has its ordinary process inbox; topology requests are handled
	// before reaching it.
	workerInbox := make(chan *relayapi.Package, 2)
	workerDetach, err := b.Attach(target, workerInbox)
	require.NoError(t, err)
	defer workerDetach()
	require.NoError(t, aTopo.Monitor(watcher, target))
	bTopo.Complete(target, &runtime.Result{
		Value: apipayload.NewString("last checkpoint"),
		Error: errors.New("controller crashed"),
	})
	select {
	case pkg := <-exits:
		defer relayapi.ReleasePackage(pkg)
		var event *topapi.ExitEvent
		for _, msg := range pkg.Messages {
			for _, pl := range msg.Payloads {
				if e, ok := pl.Data().(*topapi.ExitEvent); ok {
					event = e
				}
			}
		}
		require.NotNil(t, event)
		require.Equal(t, topapi.Exit, event.Kind)
		require.Equal(t, target, event.From)
		require.EqualError(t, event.Result.Error, "controller crashed")
		require.Equal(t, apipayload.String, event.Result.Value.Format())
		require.Equal(t, "last checkpoint", event.Result.Value.Data())
	case <-time.After(300 * time.Millisecond):
		t.Fatal("remote process exit never reached watcher")
	}
}

func TestRemoteWatcherExitReleasesRegistration(t *testing.T) {
	a := relay.NewNode("node-a")
	b := relay.NewNode("node-b")
	codec := internode.NewMessageCodec(payload.NewTranscoder())
	aToB := &wireNode{codec: codec, node: b}
	aTopo := topology.NewTopology(relay.NewRouter(a, aToB), a.ID())
	received := &countReceiver{}
	bTopo := topology.NewTopology(relay.NewRouter(b, received), b.ID())
	aToB.topo = bTopo
	watcher := pid.PID{Node: a.ID(), Host: "watcher", UniqID: "watcher"}
	target := pid.PID{Node: b.ID(), Host: "worker", UniqID: "target"}
	require.NoError(t, aTopo.Register(watcher))
	require.NoError(t, bTopo.Register(target))
	require.NoError(t, aTopo.Monitor(watcher, target))
	aTopo.Complete(watcher, &runtime.Result{})
	bTopo.Complete(target, &runtime.Result{Error: errors.New("late crash")})
	require.Zero(t, received.sends.Load(), "dead watcher must not receive remote EXIT")
}

func TestRemoteLinkRequestRegistersAndUnlinks(t *testing.T) {
	a := relay.NewNode("node-a")
	b := relay.NewNode("node-b")
	codec := internode.NewMessageCodec(payload.NewTranscoder())
	aToB := &wireNode{codec: codec, node: b}
	aTopo := topology.NewTopology(relay.NewRouter(a, aToB), a.ID())
	bTopo := topology.NewTopology(relay.NewRouter(b, &countReceiver{}), b.ID())
	aToB.topo = bTopo
	from := pid.PID{Node: a.ID(), Host: "watcher", UniqID: "watcher"}
	to := pid.PID{Node: b.ID(), Host: "worker", UniqID: "target"}
	require.NoError(t, aTopo.Register(from))
	require.NoError(t, bTopo.Register(to))
	require.NoError(t, aTopo.Link(from, to))
	links := bTopo.GetLinks(to)
	require.Len(t, links, 1)
	require.Equal(t, from.String(), links[0].String())
	require.NoError(t, aTopo.Unlink(from, to))
	require.Empty(t, bTopo.GetLinks(to))
}
