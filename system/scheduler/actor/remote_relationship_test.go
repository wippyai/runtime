// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/cluster/internode"
	syspayload "github.com/wippyai/runtime/system/payload"
	sysrelay "github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/scheduler"
	systopology "github.com/wippyai/runtime/system/topology"
	"go.uber.org/zap"
)

const (
	topicCrash  = "test.crash"
	topicFinish = "test.finish"
)

// wireLink carries packages to a peer node the way internode does: encode,
// decode on the far side, deliver to the local node.
type wireLink struct {
	codec *internode.MessageCodec
	to    *sysrelay.Node
	from  pid.NodeID
}

func (w *wireLink) Send(pkg *relay.Package) error {
	data, err := w.codec.Encode(pkg)
	if err != nil {
		return err
	}
	relay.ReleasePackage(pkg)
	decoded, err := w.codec.Decode(data)
	if err != nil {
		return err
	}
	decoded.IngressNode = w.from
	if err := w.to.Send(decoded); err != nil {
		relay.ReleasePackage(decoded)
		return err
	}
	return nil
}

// inboxWorker records every package delivered to it. A crash message fails the
// process with a typed error; a finish message returns a value.
type inboxWorker struct {
	crash    error
	topics   []string
	payloads []any
	mu       sync.Mutex
}

func (w *inboxWorker) Init(context.Context, string, payload.Payloads) error { return nil }

func (w *inboxWorker) Step(events []process.Event, out *process.StepOutput) error {
	for _, e := range events {
		if e.Type != process.EventMessage {
			continue
		}
		pkg, ok := e.Data.(*relay.Package)
		if !ok {
			continue
		}
		for _, msg := range pkg.Messages {
			w.mu.Lock()
			w.topics = append(w.topics, msg.Topic)
			for _, p := range msg.Payloads {
				w.payloads = append(w.payloads, p.Data())
			}
			w.mu.Unlock()
			switch msg.Topic {
			case topicCrash:
				return w.crash
			case topicFinish:
				out.Done(payload.NewString("last checkpoint"))
				return nil
			}
		}
	}
	out.Idle()
	return nil
}

func (w *inboxWorker) Send(*relay.Package) error { return nil }
func (w *inboxWorker) Close()                    {}

func (w *inboxWorker) received() ([]string, []any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.topics...), append([]any(nil), w.payloads...)
}

// twoNodes wires node A (watchers) to node B (a process host) through the
// internode codec.
type twoNodes struct {
	topoA   *systopology.Topology
	topoB   *systopology.Topology
	sched   *Scheduler
	routerB *sysrelay.Router
	events  chan *relay.Package
	watcher pid.PID
}

func newTwoNodes(t *testing.T) *twoNodes {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	codec := internode.NewMessageCodec(syspayload.NewTranscoder())
	nodeA := sysrelay.NewNode("node-a")
	nodeB := sysrelay.NewNode("node-b")
	routerA := sysrelay.NewRouter(nodeA, &wireLink{codec: codec, from: "node-a", to: nodeB})
	routerB := sysrelay.NewRouter(nodeB, &wireLink{codec: codec, from: "node-b", to: nodeA})
	topoA := systopology.NewTopology(routerA, nodeA.ID())
	topoB := systopology.NewTopology(routerB, nodeB.ID())

	reg := scheduler.NewRegistry()
	reg.Register(CmdComplete, CompleteHandler())
	reg.Register(CmdYield, YieldHandler())
	sched := NewScheduler(reg,
		WithWorkers(2),
		WithTopology(topoB),
		WithLifecycle(systopology.NewLifecycle(topoB, nil, zap.NewNop())),
	)
	sched.Start()
	t.Cleanup(func() { testStopScheduler(sched) })
	require.NoError(t, nodeB.RegisterHost("workers", sched))

	require.NoError(t, nodeA.RegisterHost("watchers", sysrelay.NewMailbox(ctx, sysrelay.WithBufferSize(8))))
	watcher := pid.PID{Node: nodeA.ID(), Host: "watchers", UniqID: "watcher"}
	require.NoError(t, topoA.Register(watcher))
	events := make(chan *relay.Package, 8)
	detach, err := nodeA.Attach(watcher, events)
	require.NoError(t, err)
	t.Cleanup(detach)

	return &twoNodes{topoA: topoA, topoB: topoB, sched: sched, routerB: routerB, watcher: watcher, events: events}
}

func (n *twoNodes) spawn(t *testing.T, uniq string, worker *inboxWorker) pid.PID {
	t.Helper()
	frameCtx, _ := ctxapi.OpenFrameContext(context.Background())
	target := pid.PID{Node: "node-b", Host: "workers", UniqID: uniq}
	_, err := n.sched.Submit(frameCtx, target, worker, "", nil)
	require.NoError(t, err)
	return target
}

func (n *twoNodes) tell(t *testing.T, target pid.PID, topic string) {
	t.Helper()
	require.NoError(t, n.routerB.Send(relay.NewPackage(pid.PID{}, target, topic, payload.NewString(topic))))
}

func (n *twoNodes) nextExit(t *testing.T) *topology.ExitEvent {
	t.Helper()
	select {
	case pkg := <-n.events:
		defer relay.ReleasePackage(pkg)
		require.Len(t, pkg.Messages, 1)
		require.Equal(t, topology.TopicEvents, pkg.Messages[0].Topic)
		require.Len(t, pkg.Messages[0].Payloads, 1)
		event, ok := pkg.Messages[0].Payloads[0].Data().(*topology.ExitEvent)
		require.True(t, ok, "watcher received %T", pkg.Messages[0].Payloads[0].Data())
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("remote exit never reached watcher")
		return nil
	}
}

func (n *twoNodes) requireNoEvent(t *testing.T) {
	t.Helper()
	select {
	case pkg := <-n.events:
		defer relay.ReleasePackage(pkg)
		t.Fatalf("watcher received unexpected %T", pkg.Messages[0].Payloads[0].Data())
	case <-time.After(100 * time.Millisecond):
	}
}

func requireNoTopologyRequests(t *testing.T, worker *inboxWorker) {
	t.Helper()
	topics, payloads := worker.received()
	for _, topic := range topics {
		assert.NotEqual(t, topology.TopicEvents, topic, "relationship request reached the process inbox")
	}
	for _, data := range payloads {
		switch data.(type) {
		case *topology.MonitorRequestEvent, *topology.MonitorReleaseEvent,
			*topology.LinkRequestEvent, *topology.UnlinkRequestEvent, map[string]any:
			t.Fatalf("process inbox received %T", data)
		}
	}
}

func TestRemoteMonitorReceivesTypedCrash(t *testing.T) {
	nodes := newTwoNodes(t)
	crash := apierror.New(apierror.Unavailable, "controller crashed").
		WithRetryable(apierror.True).
		WithDetails(attrs.Bag{"shard": "s-7"})
	worker := &inboxWorker{crash: crash}
	target := nodes.spawn(t, "crasher", worker)

	require.NoError(t, nodes.topoA.Monitor(nodes.watcher, target))
	nodes.tell(t, target, topicCrash)

	event := nodes.nextExit(t)
	assert.Equal(t, topology.Exit, event.Kind)
	assert.Equal(t, target.String(), event.From.String())
	require.NotNil(t, event.Result)
	require.Error(t, event.Result.Error)
	assert.Equal(t, "controller crashed", event.Result.Error.Error())
	var rich apierror.Rich
	require.True(t, errors.As(event.Result.Error, &rich))
	assert.Equal(t, apierror.Unavailable, rich.Kind())
	assert.Equal(t, apierror.True, rich.Retryable())

	requireNoTopologyRequests(t, worker)
}

func TestRemoteMonitorReceivesExitValue(t *testing.T) {
	nodes := newTwoNodes(t)
	worker := &inboxWorker{}
	target := nodes.spawn(t, "finisher", worker)

	require.NoError(t, nodes.topoA.Monitor(nodes.watcher, target))
	nodes.tell(t, target, topicFinish)

	event := nodes.nextExit(t)
	assert.Equal(t, topology.Exit, event.Kind)
	require.NotNil(t, event.Result)
	assert.NoError(t, event.Result.Error)
	require.NotNil(t, event.Result.Value)
	assert.Equal(t, payload.String, event.Result.Value.Format())
	assert.Equal(t, "last checkpoint", event.Result.Value.Data())

	requireNoTopologyRequests(t, worker)
}

func TestRemoteLinkReceivesLinkDownOnCrash(t *testing.T) {
	nodes := newTwoNodes(t)
	worker := &inboxWorker{crash: errors.New("worker failed")}
	target := nodes.spawn(t, "linked", worker)

	require.NoError(t, nodes.topoA.Link(nodes.watcher, target))
	links := nodes.topoB.GetLinks(target)
	require.Len(t, links, 1)
	assert.Equal(t, nodes.watcher.String(), links[0].String())

	nodes.tell(t, target, topicCrash)

	event := nodes.nextExit(t)
	assert.Equal(t, topology.LinkDown, event.Kind)
	assert.Equal(t, target.String(), event.From.String())
	require.NotNil(t, event.Result)
	assert.EqualError(t, event.Result.Error, "worker failed")

	requireNoTopologyRequests(t, worker)
}

func TestRemoteUnlinkRemovesLink(t *testing.T) {
	nodes := newTwoNodes(t)
	worker := &inboxWorker{crash: errors.New("worker failed")}
	target := nodes.spawn(t, "unlinked", worker)

	require.NoError(t, nodes.topoA.Link(nodes.watcher, target))
	require.Len(t, nodes.topoB.GetLinks(target), 1)
	require.NoError(t, nodes.topoA.Unlink(nodes.watcher, target))
	assert.Empty(t, nodes.topoB.GetLinks(target))

	nodes.tell(t, target, topicCrash)
	nodes.requireNoEvent(t)
	requireNoTopologyRequests(t, worker)
}

func TestRemoteMonitorReleaseStopsNotifications(t *testing.T) {
	nodes := newTwoNodes(t)
	worker := &inboxWorker{crash: errors.New("late crash")}
	target := nodes.spawn(t, "released", worker)

	require.NoError(t, nodes.topoA.Monitor(nodes.watcher, target))
	require.NoError(t, nodes.topoA.Demonitor(nodes.watcher, target))
	nodes.tell(t, target, topicCrash)

	nodes.requireNoEvent(t)
	requireNoTopologyRequests(t, worker)
}

// requireNotRegistered checks a not-registered error restored from the wire,
// which keeps kind and message but not Go error identity.
func requireNotRegistered(t *testing.T, err error) {
	t.Helper()
	var rich apierror.Rich
	require.True(t, errors.As(err, &rich), "got %T", err)
	assert.Equal(t, apierror.NotFound, rich.Kind())
	assert.Equal(t, topology.ErrPIDNotRegistered.Error(), err.Error())
	assert.Equal(t, apierror.False, rich.Retryable())
}

func TestRemoteMonitorOfUnknownTargetReceivesNoproc(t *testing.T) {
	nodes := newTwoNodes(t)
	missing := pid.PID{Node: "node-b", Host: "workers", UniqID: "missing"}

	require.NoError(t, nodes.topoA.Monitor(nodes.watcher, missing))

	event := nodes.nextExit(t)
	assert.Equal(t, topology.Exit, event.Kind)
	assert.Equal(t, missing.String(), event.From.String())
	require.NotNil(t, event.Result)
	requireNotRegistered(t, event.Result.Error)
}

func TestRemoteLinkToUnknownTargetReceivesLinkDown(t *testing.T) {
	nodes := newTwoNodes(t)
	missing := pid.PID{Node: "node-b", Host: "workers", UniqID: "missing"}

	require.NoError(t, nodes.topoA.Link(nodes.watcher, missing))

	event := nodes.nextExit(t)
	assert.Equal(t, topology.LinkDown, event.Kind)
	assert.Equal(t, missing.String(), event.From.String())
	require.NotNil(t, event.Result)
	requireNotRegistered(t, event.Result.Error)
}

func TestRelationshipRequestWithoutTopologyIsRejected(t *testing.T) {
	reg := scheduler.NewRegistry()
	reg.Register(CmdComplete, CompleteHandler())
	reg.Register(CmdYield, YieldHandler())
	sched := NewScheduler(reg, WithWorkers(1))
	sched.Start()
	defer testStopScheduler(sched)

	worker := &inboxWorker{}
	frameCtx, _ := ctxapi.OpenFrameContext(context.Background())
	target := pid.PID{Node: "node-b", Host: "workers", UniqID: "no-topology"}
	_, err := sched.Submit(frameCtx, target, worker, "", nil)
	require.NoError(t, err)

	caller := pid.PID{Node: "node-a", Host: "watchers", UniqID: "watcher"}
	err = sched.Send(topology.MonitorRequestPackage(caller, target))
	require.Error(t, err)
	assert.True(t, errors.Is(err, errTopologyUnavailable), "got %v", err)

	time.Sleep(20 * time.Millisecond)
	requireNoTopologyRequests(t, worker)
}
