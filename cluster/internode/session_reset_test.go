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
	watcher pid.PID // local process a down signal reached
	session uint64  // sender's session ID when the frame was admitted
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
				r.tl.add(timelineEntry{down: true, watcher: pkg.Target})
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
	topo       *topology.Topology
	// onFrame, when set, runs for every delivered frame on the delivery path.
	onFrame func(timelineEntry)
	id      cluster.NodeID
	// downDelay stretches applying a down signal, widening any window in
	// which it could race the delivery of later frames.
	downDelay time.Duration
}

// resetNodeOption adjusts a resetNode before it starts.
type resetNodeOption func(*resetNode)

func withDownDelay(d time.Duration) resetNodeOption { return func(n *resetNode) { n.downDelay = d } }

func withOnFrame(fn func(*resetNode, timelineEntry)) resetNodeOption {
	return func(n *resetNode) { n.onFrame = func(e timelineEntry) { fn(n, e) } }
}

// applyDown applies the down signal of an ended session to topology.
func (n *resetNode) applyDown(node cluster.NodeID) {
	time.Sleep(n.downDelay)
	n.topo.HandleNodeExit(node, errors.New("node disconnected"))
}

func startResetNode(ctx context.Context, t *testing.T, self, peer cluster.NodeID, opts ...resetNodeOption) *resetNode {
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
	for _, opt := range opts {
		opt(n)
	}
	deliver := func(pkg *relay.Package) error {
		data := pkg.Messages[0].Payloads[0].Data().([]byte)
		e := timelineEntry{session: binary.BigEndian.Uint64(data), counter: binary.BigEndian.Uint64(data[8:])}
		n.tl.add(e)
		if n.onFrame != nil {
			n.onFrame(e)
		}
		relay.ReleasePackage(pkg)
		return nil
	}
	n.service = NewService(zap.NewNop(), n.manager, rawFrameCodec{}, deliver, n.applyDown, n.bus, n.membership)

	// An ended session breaks local monitors of the peer's processes.
	n.topo = topology.NewTopology(downRouter{tl: n.tl}, self)
	watcher := pid.PID{Node: self, Host: "host", UniqID: "watcher"}
	require.NoError(t, n.topo.Register(watcher))
	require.NoError(t, n.topo.Monitor(watcher, pid.PID{Node: peer, Host: "host", UniqID: "watched"}))

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
	a := startResetNode(ctx, t, "node-a", "node-b")
	b := startResetNode(ctx, t, "node-b", "node-a")
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

// A session ends while the peer immediately sends fresh-session traffic that
// makes a local process monitor one of the peer's processes. The down signal
// of the ended session is applied before any fresh frame is delivered: the
// new monitor is never hit by it, and every down precedes the first fresh
// message.
func TestSessionEndDownPrecedesFreshSessionDelivery(t *testing.T) {
	for range 3 {
		func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var oldSessions sync.Map // node -> the peer's session ID before the reset
			freshWatcher := func(n *resetNode) pid.PID {
				return pid.PID{Node: n.id, Host: "host", UniqID: "fresh-watcher"}
			}
			var freshSeen sync.Map // node -> true once the first fresh frame arrived
			// A's reader parks inside one delivery while A's session ends, so
			// the end is signaled by that reader when it stops, concurrently
			// with the replacement session connecting.
			parked, release := make(chan struct{}), make(chan struct{})
			var park atomic.Bool
			onFrame := func(n *resetNode, e timelineEntry) {
				if n.id == "node-a" && park.CompareAndSwap(true, false) {
					close(parked)
					<-release
				}
				old, ok := oldSessions.Load(n.id)
				if !ok || e.session == old.(uint64) {
					return
				}
				if _, loaded := freshSeen.LoadOrStore(n.id, true); loaded {
					return
				}
				// The first fresh message makes a local process monitor a
				// peer process.
				peer := cluster.NodeID("node-a")
				if n.id == "node-a" {
					peer = "node-b"
				}
				require.NoError(t, n.topo.Register(freshWatcher(n)))
				require.NoError(t, n.topo.Monitor(freshWatcher(n), pid.PID{Node: peer, Host: "host", UniqID: "fresh-target"}))
				n.tl.add(timelineEntry{watcher: freshWatcher(n)})
			}
			opts := []resetNodeOption{withDownDelay(30 * time.Millisecond), withOnFrame(onFrame)}
			a := startResetNode(ctx, t, "node-a", "node-b", opts...)
			b := startResetNode(ctx, t, "node-b", "node-a", opts...)
			a.publish(cluster.NodeJoined, b.info())
			b.publish(cluster.NodeJoined, a.info())
			requireLinked(t, &trafficNode{manager: a.manager, id: a.id}, &trafficNode{manager: b.manager, id: b.id})
			oldSessions.Store(a.id, b.sessionID(a.id))
			oldSessions.Store(b.id, a.sessionID(b.id))

			stop := make(chan struct{})
			var senders sync.WaitGroup
			for _, pair := range [][2]*resetNode{{a, b}, {b, a}} {
				from, to := pair[0], pair[1]
				senders.Add(1)
				go func() {
					defer senders.Done()
					var counter uint64
					for {
						select {
						case <-stop:
							return
						default:
						}
						counter++
						// Admission is refused while the departed node is
						// not yet readmitted.
						if err := from.send(to.id, counter, ClassPGBroadcast); err != nil && !errors.Is(err, ErrNodeNotManaged) {
							t.Errorf("send: %v", err)
							return
						}
						time.Sleep(50 * time.Microsecond)
					}
				}()
			}
			require.Eventually(t, func() bool { return len(a.tl.snapshot()) > 50 && len(b.tl.snapshot()) > 50 }, 5*time.Second, time.Millisecond)
			park.Store(true)
			<-parked
			a.publish(cluster.NodeLeft, b.info())
			a.publish(cluster.NodeJoined, b.info())
			require.Eventually(t, func() bool {
				_, s := a.manager.nodeStates.GetNodeConnection(b.id)
				return s == StateConnected && a.sessionID(b.id) != 0
			}, 5*time.Second, time.Millisecond)
			time.Sleep(20 * time.Millisecond)
			close(release)
			require.Eventually(t, func() bool {
				_, fa := freshSeen.Load(a.id)
				_, fb := freshSeen.Load(b.id)
				return fa && fb
			}, 10*time.Second, time.Millisecond)
			time.Sleep(100 * time.Millisecond)
			close(stop)
			senders.Wait()

			for _, n := range []*resetNode{a, b} {
				entries := n.tl.snapshot()

				freshAt, downs := -1, 0
				for i, e := range entries {
					if e.down && e.watcher == freshWatcher(n) {
						t.Fatalf("%s: the ended session's down hit a monitor made by fresh traffic", n.id)
					}
					if !e.down && e.watcher == freshWatcher(n) {
						freshAt = i
					}
					if e.down {
						downs++
						require.Equal(t, -1, freshAt, "%s: a down of the ended session followed a fresh message", n.id)
					}
				}
				require.GreaterOrEqual(t, freshAt, 0)
				require.Equal(t, 1, downs, "%s: the old monitor receives exactly one down", n.id)
			}
		}()
	}
}

// A sequence gap is a session failure, not a dead link: the receiving side
// ends the session and signals the peer down, the peer ends its session too,
// and fresh traffic flows again in both directions.
func TestSequenceGapEndsSessionAndRecovers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startResetNode(ctx, t, "node-a", "node-b")
	b := startResetNode(ctx, t, "node-b", "node-a")
	a.publish(cluster.NodeJoined, b.info())
	b.publish(cluster.NodeJoined, a.info())
	requireLinked(t, &trafficNode{manager: a.manager, id: a.id}, &trafficNode{manager: b.manager, id: b.id})
	require.NoError(t, b.send(a.id, 1, ClassRaftControl))
	require.Eventually(t, func() bool { return len(a.tl.snapshot()) == 1 }, 5*time.Second, time.Millisecond)

	// B skips a sequence number: the next frame arrives with a gap.
	state := b.manager.nodeStates.GetNodeState(a.id)
	state.queueMu.Lock()
	state.session.sendNext++
	state.queueMu.Unlock()
	require.NoError(t, b.send(a.id, 2, ClassRaftControl))

	require.Eventually(t, func() bool { return a.tl.downs() == 1 && b.tl.downs() == 1 }, 10*time.Second, time.Millisecond)
	requireFreshTraffic(t, b, a)
	requireFreshTraffic(t, a, b)
	require.Equal(t, 1, a.tl.downs())
	require.Equal(t, 1, b.tl.downs())
}

// A departure breaks local monitors of the node's processes whether or not a
// session with the node ever connected.
func TestDepartureWithoutConnectedSessionBreaksMonitors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Managed, dialed, never connected.
	a := startResetNode(ctx, t, "node-a", "node-b")
	unreachable := cluster.NodeInfo{ID: "node-b", Addr: "127.0.0.1", Meta: cluster.NodeMeta{
		MetadataPort:            strconv.Itoa(closedLocalPort(t)),
		cluster.MetaIncarnation: "5",
	}}
	a.publish(cluster.NodeJoined, unreachable)
	require.Eventually(t, func() bool { return a.manager.IsManaged("node-b") }, 2*time.Second, time.Millisecond)
	a.publish(cluster.NodeLeft, unreachable)
	require.Eventually(t, func() bool { return a.tl.downs() == 1 }, 5*time.Second, time.Millisecond)

	// Never managed at all.
	c := startResetNode(ctx, t, "node-c", "node-d")
	c.publish(cluster.NodeLeft, cluster.NodeInfo{ID: "node-d"})
	require.Eventually(t, func() bool { return c.tl.downs() == 1 }, 5*time.Second, time.Millisecond)
}
