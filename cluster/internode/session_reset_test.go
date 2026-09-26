// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"encoding/binary"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	topoapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/topology"
	"go.uber.org/zap"
)

// timelineEntry is one observation on a node: a delivered frame or a down
// signal reaching a local watcher.
type timelineEntry struct {
	session uint64 // sender's session ID when the frame was admitted
	counter uint64
	down    bool
}

// timeline is a node's ordered record of deliveries and down signals.
type timeline struct {
	entries []timelineEntry
	mu      sync.Mutex
}

func (tl *timeline) add(e timelineEntry) {
	tl.mu.Lock()
	tl.entries = append(tl.entries, e)
	tl.mu.Unlock()
}

func (tl *timeline) snapshot() []timelineEntry {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	return append([]timelineEntry(nil), tl.entries...)
}

func (tl *timeline) downs() int {
	n := 0
	for _, e := range tl.snapshot() {
		if e.down {
			n++
		}
	}
	return n
}

// rawFrameCodec carries the raw frame bytes as the package's only payload.
type rawFrameCodec struct{}

func (rawFrameCodec) Encode(*relay.Package) ([]byte, error) {
	return nil, errors.New("rawFrameCodec does not encode")
}

func (rawFrameCodec) Decode(data []byte) (*relay.Package, error) {
	pkg := relay.AcquirePackage()
	msg := relay.AcquireMessage()
	msg.Payloads = payload.Payloads{payload.New(append([]byte(nil), data...))}
	pkg.Messages = append(pkg.Messages[:0], msg)
	return pkg, nil
}

// downRouter stands in for the local relay under topology: it records link
// down events reaching local watchers and accepts every other package.
type downRouter struct{ tl *timeline }

func (r downRouter) Send(pkg *relay.Package) error {
	for _, msg := range pkg.Messages {
		if msg.Topic != topoapi.TopicEvents {
			continue
		}
		for _, p := range msg.Payloads {
			if exit, ok := p.Data().(*topoapi.ExitEvent); ok && exit.Kind == topoapi.LinkDown {
				r.tl.add(timelineEntry{down: true})
			}
		}
	}
	return nil
}

// resetNode is a full internode endpoint: manager, service, bus, and a
// topology whose local process monitors a process on the peer.
type resetNode struct {
	// ambiguous holds counters admitted while the session changed; which
	// session carried them is unknown.
	ambiguous  sync.Map
	manager    *manager
	service    *Service
	bus        *eventbus.Bus
	membership *mockMembership
	tl         *timeline
	id         cluster.NodeID
}

func startResetNode(t *testing.T, ctx context.Context, self, peer cluster.NodeID) *resetNode {
	t.Helper()
	cfg := insecureManagerConfig()
	cfg.LocalNodeID = self
	cfg.BindAddr = "127.0.0.1"
	cfg.BindPort = 0
	cfg.Logger = zap.NewNop()
	cfg.InitialRetryDelay = time.Millisecond
	cfg.MaxRetryDelay = 10 * time.Millisecond
	n := &resetNode{
		id:         self,
		manager:    NewConnectionManager(cfg, nil).(*manager),
		bus:        eventbus.NewBus(),
		membership: &mockMembership{localNode: cluster.NodeInfo{ID: self, Addr: "127.0.0.1"}},
		tl:         &timeline{},
	}
	deliver := func(pkg *relay.Package) error {
		data := pkg.Messages[0].Payloads[0].Data().([]byte)
		n.tl.add(timelineEntry{session: binary.BigEndian.Uint64(data), counter: binary.BigEndian.Uint64(data[8:])})
		relay.ReleasePackage(pkg)
		return nil
	}
	n.service = NewService(zap.NewNop(), n.manager, rawFrameCodec{}, deliver, n.bus, n.membership)

	// The topology listener: an ended session breaks local monitors of the
	// peer's processes.
	topo := topology.NewTopology(downRouter{tl: n.tl}, self)
	watcher := pid.PID{Node: self, Host: "host", UniqID: "watcher"}
	require.NoError(t, topo.Register(watcher))
	require.NoError(t, topo.Monitor(watcher, pid.PID{Node: peer, Host: "host", UniqID: "watched"}))
	sub, err := eventbus.NewSubscriber(ctx, n.bus, cluster.System, cluster.NodeSessionEnded, func(e event.Event) {
		topo.HandleNodeExit(e.Path, errors.New("node disconnected"))
	})
	require.NoError(t, err)
	t.Cleanup(sub.Close)

	require.NoError(t, n.service.Start(ctx))
	t.Cleanup(func() { require.NoError(t, n.service.Stop()) })
	return n
}

// info is how membership describes this node to its peer.
func (n *resetNode) info() cluster.NodeInfo {
	return cluster.NodeInfo{ID: n.id, Addr: "127.0.0.1", Meta: cluster.NodeMeta{
		MetadataPort:            strconv.Itoa(n.manager.GetListenPort()),
		cluster.MetaIncarnation: strconv.FormatUint(n.manager.Incarnation(), 10),
	}}
}

func (n *resetNode) publish(kind event.Kind, node cluster.NodeInfo) {
	n.bus.Send(context.Background(), event.Event{System: cluster.System, Kind: kind, Path: node.ID, Data: cluster.NodeEvent{Node: node}})
}

// sessionID reads the node's current session ID with peer.
func (n *resetNode) sessionID(peer cluster.NodeID) uint64 {
	state := n.manager.nodeStates.GetNodeState(peer)
	if state == nil {
		return 0
	}
	state.queueMu.Lock()
	defer state.queueMu.Unlock()
	return state.session.id
}

// send queues a frame tagged with the session current at admission. A frame
// admitted while the session changed is recorded as ambiguous.
func (n *resetNode) send(peer cluster.NodeID, counter uint64, class Class) error {
	before := n.sessionID(peer)
	data := make([]byte, 16)
	binary.BigEndian.PutUint64(data, before)
	binary.BigEndian.PutUint64(data[8:], counter)
	if err := n.manager.SendToNode(peer, data, class); err != nil {
		return err
	}
	if n.sessionID(peer) != before {
		n.ambiguous.Store(counter, true)
	}
	return nil
}

// A one-sided departure: A's membership drops B and readmits it while B
// stays up, never sees A leave, and keeps sending. After the reset:
//   - no frame B sent in its old session reaches A after A's down signal;
//   - B's local monitors of A's processes receive the down signal;
//   - fresh traffic flows exactly once and in order in both directions.
func TestOneSidedDepartureEndsSessionOnBothSides(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startResetNode(t, ctx, "node-a", "node-b")
	b := startResetNode(t, ctx, "node-b", "node-a")
	a.publish(cluster.NodeJoined, b.info())
	b.publish(cluster.NodeJoined, a.info())
	requireLinked(t, &trafficNode{manager: a.manager, id: a.id}, &trafficNode{manager: b.manager, id: b.id})

	oldSession := b.sessionID(a.id)
	require.NotZero(t, oldSession)

	var counter atomic.Uint64
	stop := make(chan struct{})
	sending := make(chan struct{})
	go func() {
		defer close(sending)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := b.send(a.id, counter.Add(1), ClassPGBroadcast); err != nil {
				t.Errorf("send: %v", err)
				return
			}
			time.Sleep(20 * time.Microsecond)
		}
	}()

	require.Eventually(t, func() bool { return len(a.tl.snapshot()) > 200 }, 5*time.Second, time.Millisecond)
	a.publish(cluster.NodeLeft, b.info())
	a.publish(cluster.NodeJoined, b.info())

	// Both sides signal the peer down exactly once.
	require.Eventually(t, func() bool { return a.tl.downs() == 1 && b.tl.downs() == 1 }, 10*time.Second, time.Millisecond)
	// B's session was replaced and the link is back.
	require.Eventually(t, func() bool {
		sid := b.sessionID(a.id)
		if sid == oldSession || sid == 0 {
			return false
		}
		_, as := a.manager.nodeStates.GetNodeConnection(b.id)
		_, bs := b.manager.nodeStates.GetNodeConnection(a.id)
		return as == StateConnected && bs == StateConnected
	}, 10*time.Second, time.Millisecond)
	close(stop)
	<-sending

	// (a) Nothing of B's old session is delivered after A's down signal.
	entries := a.tl.snapshot()
	downAt := -1
	for i, e := range entries {
		if e.down {
			downAt = i
			break
		}
	}
	require.GreaterOrEqual(t, downAt, 0)
	for _, e := range entries[downAt+1:] {
		if _, ambiguous := b.ambiguous.Load(e.counter); ambiguous {
			continue
		}
		require.NotEqual(t, oldSession, e.session, "frame %d of the ended session delivered after the down signal", e.counter)
	}

	// Within each session, frames arrive at most once and in order.
	last := map[uint64]uint64{}
	for _, e := range entries {
		if e.down {
			continue
		}
		require.Greater(t, e.counter, last[e.session], "duplicate or reordered frame in session %x", e.session)
		last[e.session] = e.counter
	}

	// (c) Fresh traffic flows exactly once and in order in both directions.
	requireFreshTraffic(t, b, a)
	requireFreshTraffic(t, a, b)
	// (b) The down signal fired once per side and no further.
	require.Equal(t, 1, a.tl.downs())
	require.Equal(t, 1, b.tl.downs())
}

// requireFreshTraffic sends a batch from one node and requires the peer to
// deliver all of it exactly once and in order.
func requireFreshTraffic(t *testing.T, from, to *resetNode) {
	t.Helper()
	const batch = 1000
	base := uint64(1) << 40
	session := from.sessionID(to.id)
	for i := uint64(0); i < batch; i++ {
		require.NoError(t, from.send(to.id, base+i, ClassRaftControl))
	}
	require.Equal(t, session, from.sessionID(to.id), "session changed during fresh traffic")
	fresh := func() []uint64 {
		var got []uint64
		for _, e := range to.tl.snapshot() {
			if !e.down && e.counter >= base {
				got = append(got, e.counter)
			}
		}
		return got
	}
	require.Eventually(t, func() bool { return len(fresh()) >= batch }, 10*time.Second, time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	got := fresh()
	require.Len(t, got, batch)
	for i, c := range got {
		require.Equal(t, base+uint64(i), c)
	}
}
