// SPDX-License-Identifier: MPL-2.0

package attempt

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	cgapi "github.com/wippyai/runtime/api/service/cloudgraph"
	topapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/service/cloudgraph/binding"
	"github.com/wippyai/runtime/service/cloudgraph/ledger"
	"github.com/wippyai/runtime/service/cloudgraph/resource"
)

type jsonTranscoder struct{}

func (jsonTranscoder) Unmarshal(p payload.Payload, v any) error {
	raw, err := json.Marshal(p.Data())
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

func (jsonTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

type fakePIDGen struct{ counter int }

func (g *fakePIDGen) Generate(host pid.HostID) pid.PID {
	g.counter++
	return pid.PID{Host: host, UniqID: string(rune('a' + g.counter))}
}

type fakeTopo struct{}

func (fakeTopo) Register(pid.PID) error { return nil }
func (fakeTopo) Remove(pid.PID)         {}

type fakeNode struct {
	channels map[string]chan *relay.Package
	sent     []*relay.Package
	mu       sync.Mutex
}

func newFakeNode() *fakeNode {
	return &fakeNode{channels: make(map[string]chan *relay.Package)}
}

func (n *fakeNode) Attach(p pid.PID, ch chan *relay.Package) (context.CancelFunc, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.channels[p.String()] = ch
	return func() {}, nil
}

func (n *fakeNode) Send(pkg *relay.Package) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sent = append(n.sent, pkg)
	return nil
}

func (n *fakeNode) channelFor(p pid.PID) chan *relay.Package {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.channels[p.String()]
}

// actorScript drives the scripted provider actor: it receives the spawn
// input and a send function that delivers envelopes to the collector.
type actorScript func(input map[string]any, send func(wire map[string]any), exit func())

type fakeProcs struct {
	node    *fakeNode
	script  actorScript
	started []map[string]any
	mu      sync.Mutex
}

func (f *fakeProcs) Start(_ context.Context, start *process.Start) (pid.PID, error) {
	var input map[string]any
	if len(start.Input) > 0 {
		raw, err := json.Marshal(start.Input[0].Data())
		if err != nil {
			return pid.PID{}, err
		}
		if err := json.Unmarshal(raw, &input); err != nil {
			return pid.PID{}, err
		}
	}

	f.mu.Lock()
	f.started = append(f.started, input)
	f.mu.Unlock()

	collector, _ := input["collector"].(string)
	collectorPID, err := pid.ParsePID(collector)
	if err != nil {
		return pid.PID{}, err
	}
	ch := f.node.channelFor(collectorPID)

	send := func(wire map[string]any) {
		pkg := &relay.Package{}
		pkg.AddMessage(SignalTopic, payload.New(wire))
		ch <- pkg
	}
	exit := func() {
		pkg := &relay.Package{}
		pkg.AddMessage(topapi.TopicEvents, payload.New(&topapi.ExitEvent{}))
		ch <- pkg
	}

	go f.script(input, send, exit)
	return pid.PID{Host: "actors", UniqID: "actor-1"}, nil
}

func (f *fakeProcs) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.started)
}

type execEnv struct {
	store *ledger.Store
	procs *fakeProcs
	node  *fakeNode
	exec  *Executor
}

func newExecEnv(t *testing.T, script actorScript) *execEnv {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+t.TempDir()+"/ledger.db?_busy_timeout=5000&_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := ledger.NewStore(db, nil)
	require.NoError(t, store.Migrate(context.Background()))

	providers := binding.NewRegistry()
	require.NoError(t, providers.Register(&cgapi.ProviderConfig{
		ProviderType:  "docker",
		ActorProcess:  registry.NewID("poc", "docker_actor"),
		Executor:      registry.NewID("poc", "exec"),
		ResourceTypes: []string{"container"},
	}))

	node := newFakeNode()
	procs := &fakeProcs{node: node, script: script}

	exec := NewExecutor(Deps{
		PIDGen:     &fakePIDGen{},
		Node:       node,
		Topo:       fakeTopo{},
		Procs:      procs,
		Transcoder: jsonTranscoder{},
		Store:      store,
		Providers:  providers,
		HostID:     "poc:processes",
	},
		WithGateTimeout(2*time.Second),
		WithOperationTimeout(10*time.Second),
		WithPollInterval(20*time.Millisecond),
		WithExitGrace(100*time.Millisecond),
	)

	return &execEnv{store: store, procs: procs, node: node, exec: exec}
}

func (env *execEnv) seed(t *testing.T, deployID string, specs []resource.Spec, edges []resource.Edge) {
	t.Helper()
	ctx := context.Background()

	created, err := env.store.CreateDeploy(ctx, resource.DeployInput{DeployID: deployID, Scope: "poc", Specs: specs})
	require.NoError(t, err)
	require.True(t, created)

	ops := make([]resource.Operation, len(specs))
	for i, spec := range specs {
		ops[i] = resource.Operation{
			ID:         deployID + "/" + spec.ResourceID,
			DeployID:   deployID,
			ResourceID: spec.ResourceID,
			Action:     resource.ActionCreate,
		}
	}

	require.NoError(t, env.store.SavePlan(ctx, &ledger.PlanRecord{
		ID:         "plan/" + deployID,
		DeployID:   deployID,
		Operations: json.RawMessage(`[]`),
		Edges:      json.RawMessage(`[]`),
		Waves:      json.RawMessage(`[]`),
		SpecHash:   "hash",
	}, ops, edges))
}

func happyScript(outputs string) actorScript {
	return func(_ map[string]any, send func(map[string]any), _ func()) {
		send(map[string]any{"seq": 1, "kind": "phase", "phase": "allocate_started"})
		send(map[string]any{"seq": 2, "kind": "phase", "phase": "allocate_committed"})
		send(map[string]any{"seq": 3, "kind": "checkpoint", "payload": map[string]any{"stage": "configured"}})
		send(map[string]any{"seq": 4, "kind": "phase", "phase": "verify_passed",
			"payload": map[string]any{"outputs": json.RawMessage(outputs)}})
	}
}

func TestExecuteHappyPath(t *testing.T) {
	env := newExecEnv(t, happyScript(`{"container_id":"abc","dsn":"postgres://x"}`))
	env.seed(t, "d1", []resource.Spec{{
		ResourceID:   "poc/postgres",
		Type:         "container",
		ProviderType: "docker",
		Desired:      json.RawMessage(`{"image":"postgres:18-alpine"}`),
	}}, nil)

	result, err := env.exec.Execute(context.Background(), Request{OpID: "d1/poc/postgres", Incarnation: 1})
	require.NoError(t, err)
	require.Contains(t, string(result), "container_id")

	op, err := env.store.GetOperation(context.Background(), "d1/poc/postgres")
	require.NoError(t, err)
	require.Equal(t, resource.OpCommitted, op.Status)

	inst, err := env.store.GetInstance(context.Background(), "poc/postgres")
	require.NoError(t, err)
	require.Equal(t, "active", inst.Lifecycle)
	require.Contains(t, string(inst.Outputs), "dsn")

	signals, err := env.store.ListSignals(context.Background(), "d1/poc/postgres", 1)
	require.NoError(t, err)
	require.Len(t, signals, 4)
}

func TestExecuteTerminalShortCircuit(t *testing.T) {
	env := newExecEnv(t, happyScript(`{"container_id":"abc"}`))
	env.seed(t, "d1", []resource.Spec{{
		ResourceID: "poc/app", Type: "container", ProviderType: "docker",
	}}, nil)

	_, err := env.exec.Execute(context.Background(), Request{OpID: "d1/poc/app", Incarnation: 1})
	require.NoError(t, err)
	require.Equal(t, 1, env.procs.startCount())

	result, err := env.exec.Execute(context.Background(), Request{OpID: "d1/poc/app", Incarnation: 2})
	require.NoError(t, err)
	require.Contains(t, string(result), "poc/app")
	require.Equal(t, 1, env.procs.startCount(), "terminal operation must not respawn the actor")
}

func TestExecuteStaleIncarnation(t *testing.T) {
	env := newExecEnv(t, happyScript(`{}`))
	env.seed(t, "d1", []resource.Spec{{
		ResourceID: "poc/app", Type: "container", ProviderType: "docker",
	}}, nil)

	ok, err := env.store.BumpIncarnation(context.Background(), "d1/poc/app", 5)
	require.NoError(t, err)
	require.True(t, ok)

	_, err = env.exec.Execute(context.Background(), Request{OpID: "d1/poc/app", Incarnation: 2})
	require.Error(t, err)
	require.Contains(t, err.Error(), "stale incarnation")
	require.Equal(t, 0, env.procs.startCount())
}

func TestExecuteProviderFailure(t *testing.T) {
	env := newExecEnv(t, func(_ map[string]any, send func(map[string]any), _ func()) {
		send(map[string]any{"seq": 1, "kind": "phase", "phase": "allocate_started"})
		send(map[string]any{"seq": 2, "kind": "failed",
			"payload": map[string]any{"error": "image pull failed"}})
	})
	env.seed(t, "d1", []resource.Spec{{
		ResourceID: "poc/app", Type: "container", ProviderType: "docker",
	}}, nil)

	_, err := env.exec.Execute(context.Background(), Request{OpID: "d1/poc/app", Incarnation: 1})
	require.Error(t, err)
	require.Contains(t, err.Error(), "image pull failed")

	op, err := env.store.GetOperation(context.Background(), "d1/poc/app")
	require.NoError(t, err)
	require.Equal(t, resource.OpFailed, op.Status)
	require.Equal(t, "image pull failed", op.Error)
}

func TestExecuteActorCrashThenRetryResumes(t *testing.T) {
	firstRun := true
	env := newExecEnv(t, nil)
	env.procs.script = func(input map[string]any, send func(map[string]any), exit func()) {
		if firstRun {
			firstRun = false
			send(map[string]any{"seq": 1, "kind": "phase", "phase": "allocate_started"})
			send(map[string]any{"seq": 2, "kind": "checkpoint", "payload": map[string]any{"stage": "allocated"}})
			exit()
			return
		}
		if input["checkpoint"] == nil {
			send(map[string]any{"seq": 1, "kind": "failed",
				"payload": map[string]any{"error": "expected checkpoint on respawn"}})
			return
		}
		send(map[string]any{"seq": 1, "kind": "phase", "phase": "configure_committed"})
		send(map[string]any{"seq": 2, "kind": "phase", "phase": "verify_passed",
			"payload": map[string]any{"outputs": map[string]any{"container_id": "resumed"}}})
	}

	env.seed(t, "d1", []resource.Spec{{
		ResourceID: "poc/app", Type: "container", ProviderType: "docker",
	}}, nil)

	_, err := env.exec.Execute(context.Background(), Request{OpID: "d1/poc/app", Incarnation: 1})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exited without terminal signal")

	result, err := env.exec.Execute(context.Background(), Request{OpID: "d1/poc/app", Incarnation: 2})
	require.NoError(t, err)
	require.Contains(t, string(result), "resumed")
	require.Equal(t, 2, env.procs.startCount())
}

func TestExecuteGateBlocksThenReleases(t *testing.T) {
	env := newExecEnv(t, happyScript(`{"container_id":"app"}`))
	specs := []resource.Spec{
		{ResourceID: "poc/app", Type: "container", ProviderType: "docker",
			Desired: json.RawMessage(`{"env":{"DATABASE_URL":{"$ref":{"resource_id":"poc/postgres","output_key":"dsn"}}}}`)},
		{ResourceID: "poc/postgres", Type: "container", ProviderType: "docker",
			Desired: json.RawMessage(`{"image":"postgres"}`)},
	}
	env.seed(t, "d1", specs, []resource.Edge{
		{SourceResourceID: "poc/app", TargetResourceID: "poc/postgres", Kind: resource.DepConfigure},
	})

	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		_, err := env.exec.Execute(ctx, Request{OpID: "d1/poc/app", Incarnation: 1})
		done <- err
	}()

	time.Sleep(150 * time.Millisecond)
	require.Equal(t, 0, env.procs.startCount(), "actor must not spawn while the gate is unsatisfied")

	pgOp, err := env.store.GetOperation(ctx, "d1/poc/postgres")
	require.NoError(t, err)
	ok, err := env.store.BumpIncarnation(ctx, pgOp.ID, 1)
	require.NoError(t, err)
	require.True(t, ok)
	pgOp.Incarnation = 1
	pgSpec, err := env.store.GetSpec(ctx, "d1", "poc/postgres")
	require.NoError(t, err)
	require.NoError(t, env.store.CommitOperation(ctx, pgOp, pgSpec, "poc",
		json.RawMessage(`{"dsn":"postgres://db:5432/app"}`), nil))

	require.NoError(t, <-done)

	require.Equal(t, 1, env.procs.startCount())
	input := env.procs.started[0]
	desired := input["desired"].(map[string]any)
	env2 := desired["env"].(map[string]any)
	require.Equal(t, "postgres://db:5432/app", env2["DATABASE_URL"], "$ref must be materialized from committed outputs")
}

func TestExecuteGateTimeout(t *testing.T) {
	env := newExecEnv(t, happyScript(`{}`))
	env.seed(t, "d1", []resource.Spec{
		{ResourceID: "poc/app", Type: "container", ProviderType: "docker"},
		{ResourceID: "poc/postgres", Type: "container", ProviderType: "docker"},
	}, []resource.Edge{
		{SourceResourceID: "poc/app", TargetResourceID: "poc/postgres", Kind: resource.DepCreate},
	})

	fast := NewExecutor(env.exec.deps,
		WithGateTimeout(100*time.Millisecond),
		WithPollInterval(20*time.Millisecond),
	)

	_, err := fast.Execute(context.Background(), Request{OpID: "d1/poc/app", Incarnation: 1})
	require.Error(t, err)
	require.Contains(t, err.Error(), "gate timeout")

	op, err := env.store.GetOperation(context.Background(), "d1/poc/app")
	require.NoError(t, err)
	require.Equal(t, resource.OpFailed, op.Status)
}

func TestExecuteFailedDependencyFailsFast(t *testing.T) {
	env := newExecEnv(t, happyScript(`{}`))
	env.seed(t, "d1", []resource.Spec{
		{ResourceID: "poc/app", Type: "container", ProviderType: "docker"},
		{ResourceID: "poc/postgres", Type: "container", ProviderType: "docker"},
	}, []resource.Edge{
		{SourceResourceID: "poc/app", TargetResourceID: "poc/postgres", Kind: resource.DepCreate},
	})

	ctx := context.Background()
	ok, err := env.store.BumpIncarnation(ctx, "d1/poc/postgres", 1)
	require.NoError(t, err)
	require.True(t, ok)
	done, err := env.store.Terminalize(ctx, "d1/poc/postgres", 1,
		[]resource.OperationStatus{resource.OpDispatched}, resource.OpFailed, "boom")
	require.NoError(t, err)
	require.True(t, done)

	_, err = env.exec.Execute(ctx, Request{OpID: "d1/poc/app", Incarnation: 1})
	require.Error(t, err)
	require.Contains(t, err.Error(), "dependency poc/postgres failed")
	require.Equal(t, 0, env.procs.startCount())
}
