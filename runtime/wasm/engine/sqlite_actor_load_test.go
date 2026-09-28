// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero"
	wazeroapi "github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	relayapi "github.com/wippyai/runtime/api/relay"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	secapi "github.com/wippyai/runtime/api/security"
	hostapi "github.com/wippyai/runtime/api/service/host"
	"github.com/wippyai/runtime/internal/uniqid"
	actorhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
	clihost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/cli"
	clockhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/clocks"
	fshost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/filesystem"
	iohost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/io"
	randhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/random"
	stdiohost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/stdio"
	servicehost "github.com/wippyai/runtime/service/host"
	sysprocess "github.com/wippyai/runtime/system/process"
	sysrelay "github.com/wippyai/runtime/system/relay"
	actorsched "github.com/wippyai/runtime/system/scheduler/actor"
	secsys "github.com/wippyai/runtime/system/security"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

// sqliteFixtureCompilationCache shares only compiled SQLite fixture code. Each
// actor still receives a fresh runtime, host registry, resource table, module
// instance, and guest memory.
var sqliteFixtureCompilationCache = wazero.NewCompilationCache()

func TestMain(m *testing.M) {
	code := m.Run()
	if err := sqliteFixtureCompilationCache.Close(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "close SQLite fixture compilation cache: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

// --- Harness Infrastructure: Scheduler Host, Relay, and Client ---

type harnessLifecycle struct {
	completeChs map[string]chan *runtimeapi.Result
	completed   map[string]*runtimeapi.Result
	counts      map[string]uint32
	mu          sync.Mutex
}

func newHarnessLifecycle() *harnessLifecycle {
	return &harnessLifecycle{
		completeChs: make(map[string]chan *runtimeapi.Result),
		completed:   make(map[string]*runtimeapi.Result),
		counts:      make(map[string]uint32),
	}
}

func (l *harnessLifecycle) registerWait(target pid.PID) chan *runtimeapi.Result {
	l.mu.Lock()
	defer l.mu.Unlock()
	ch := make(chan *runtimeapi.Result, 1)
	if res, ok := l.completed[target.String()]; ok {
		ch <- res
		return ch
	}
	l.completeChs[target.String()] = ch
	return ch
}

func (l *harnessLifecycle) OnStart(_ context.Context, _ pid.PID, _ processapi.Process) error {
	return nil
}

func (l *harnessLifecycle) completionCount(target pid.PID) uint32 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.counts[target.String()]
}

func (l *harnessLifecycle) OnComplete(_ context.Context, p pid.PID, result *runtimeapi.Result) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.completed[p.String()] = result
	l.counts[p.String()]++
	if ch, ok := l.completeChs[p.String()]; ok {
		ch <- result
		delete(l.completeChs, p.String())
	}
}

type harnessClientReceiver struct {
	chans   map[string]chan *relayapi.Message
	mu      sync.RWMutex
	dropped atomic.Uint64
}

func newHarnessClientReceiver() *harnessClientReceiver {
	return &harnessClientReceiver{
		chans: make(map[string]chan *relayapi.Message),
	}
}

func (r *harnessClientReceiver) registerClient(clientUniqID string, ch chan *relayapi.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chans[clientUniqID] = ch
}

func (r *harnessClientReceiver) unregisterClient(clientUniqID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.chans, clientUniqID)
}

func (r *harnessClientReceiver) Send(pkg *relayapi.Package) error {
	if pkg == nil || len(pkg.Messages) == 0 {
		return nil
	}
	r.mu.RLock()
	ch, ok := r.chans[pkg.Target.UniqID]
	r.mu.RUnlock()

	for _, msg := range pkg.Messages {
		if ok {
			select {
			case ch <- msg:
			default:
				r.dropped.Add(1)
			}
		} else {
			r.dropped.Add(1)
		}
	}
	return nil
}

type harnessCommandRegistry struct {
	handlers map[dispatcher.CommandID]dispatcher.Handler
}

func (r *harnessCommandRegistry) Get(id dispatcher.CommandID) dispatcher.Handler {
	if r == nil || r.handlers == nil {
		return nil
	}
	return r.handlers[id]
}

func (r *harnessCommandRegistry) Has(id dispatcher.CommandID) bool {
	if r == nil || r.handlers == nil {
		return false
	}
	_, ok := r.handlers[id]
	return ok
}

type testFactory struct {
	createFunc  func() (processapi.Process, error)
	workerClass string
	method      string
}

func (f *testFactory) Create(_ registry.ID) (processapi.Process, *processapi.Meta, error) {
	proc, err := f.createFunc()
	if err != nil {
		return nil, nil, err
	}
	meta := &processapi.Meta{
		WorkerClass: f.workerClass,
		Method:      f.method,
	}
	return proc, meta, nil
}

type harnessCluster struct {
	node      *sysrelay.Node
	router    *sysrelay.Router
	host      *servicehost.Host
	scheduler *actorsched.Scheduler
	receiver  *harnessClientReceiver
	lifecycle *harnessLifecycle
	hostID    registry.ID
	hostPID   pid.HostID
	nodeID    pid.NodeID
}

// sqliteHarnessPolicy permits replies only to clients registered in this
// harness. Network, filesystem and unrelated process permissions remain denied.
type sqliteHarnessPolicy struct{ receiver *harnessClientReceiver }

func (sqliteHarnessPolicy) ID() registry.ID { return registry.NewID("test", "sqlite-replies") }
func (p sqliteHarnessPolicy) Evaluate(actor secapi.Actor, action, resource string, _ attrs.Bag) secapi.Result {
	if actor.ID != "sqlite-harness" || action != "process.send" {
		return secapi.Deny
	}
	target, err := pid.ParsePID(resource)
	if err != nil || target.Node != "local" || target.Host != "client" {
		return secapi.Deny
	}
	p.receiver.mu.RLock()
	_, exists := p.receiver.chans[target.UniqID]
	p.receiver.mu.RUnlock()
	if exists {
		return secapi.Allow
	}
	return secapi.Deny
}

func sqliteHarnessContext(t testing.TB, receiver *harnessClientReceiver) context.Context {
	t.Helper()
	ctx, frame := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	t.Cleanup(func() { require.NoError(t, frame.Close()) })
	require.NoError(t, secapi.SetActor(ctx, secapi.Actor{ID: "sqlite-harness"}))
	require.NoError(t, secapi.SetScope(ctx, secsys.NewScope([]secapi.Policy{sqliteHarnessPolicy{receiver: receiver}})))
	return ctx
}

func TestSQLiteHarnessReplyAuthorization(t *testing.T) {
	receiver := newHarnessClientReceiver()
	receiver.registerClient("allowed", make(chan *relayapi.Message, 1))
	ctx := sqliteHarnessContext(t, receiver)
	for _, tc := range []struct {
		name, action string
		target       pid.PID
		want         bool
	}{
		{"registered-client", "process.send", pid.PID{Node: "local", Host: "client", UniqID: "allowed"}, true},
		{"unregistered-client", "process.send", pid.PID{Node: "local", Host: "client", UniqID: "unknown"}, false},
		{"remote-node", "process.send", pid.PID{Node: "remote", Host: "client", UniqID: "allowed"}, false},
		{"other-host", "process.send", pid.PID{Node: "local", Host: "other", UniqID: "allowed"}, false},
		{"other-action", "process.spawn", pid.PID{Node: "local", Host: "client", UniqID: "allowed"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, secapi.IsAllowed(ctx, tc.action, tc.target.String(), nil))
		})
	}
	receiver.unregisterClient("allowed")
	require.False(t, secapi.IsAllowed(ctx, "process.send", (&pid.PID{Node: "local", Host: "client", UniqID: "allowed"}).String(), nil))
}

func newHarnessCluster(t testing.TB, workers int, factoryFunc func() (processapi.Process, error)) *harnessCluster {
	return newHarnessClusterWithCommands(t, workers, factoryFunc, nil)
}

// newHarnessClusterWithCommands installs additional service command handlers into
// the same registry consumed by the production actor scheduler.
func newHarnessClusterWithCommands(t testing.TB, workers int, factoryFunc func() (processapi.Process, error), registerCommands func(func(dispatcher.CommandID, dispatcher.Handler))) *harnessCluster {
	t.Helper()
	nodeID := pid.NodeID("local")
	hostID := registry.NewID("wasm", "actors")
	hostPID := hostID.String()

	node := sysrelay.NewNode(nodeID)
	router := sysrelay.NewRouter(node, nil)
	lifecycle := newHarnessLifecycle()
	clientReceiver := newHarnessClientReceiver()

	require.NoError(t, node.RegisterHost("client", clientReceiver))

	cmdRegistry := &harnessCommandRegistry{handlers: make(map[dispatcher.CommandID]dispatcher.Handler)}
	disp := sysprocess.NewDispatcher(nil, router, nil, zap.NewNop())
	disp.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		cmdRegistry.handlers[id] = h
	})
	if registerCommands != nil {
		registerCommands(func(id dispatcher.CommandID, h dispatcher.Handler) {
			cmdRegistry.handlers[id] = h
		})
	}

	schedOpts := []actorsched.Option{
		actorsched.WithWorkers(workers),
		actorsched.WithQueueSize(4096),
		actorsched.WithLocalQueueSize(512),
		actorsched.WithDedicatedThreads(),
		actorsched.WithLifecycle(lifecycle),
	}
	scheduler := actorsched.NewScheduler(cmdRegistry, schedOpts...)

	factory := &testFactory{
		createFunc:  factoryFunc,
		workerClass: hostapi.WorkerClassWASM,
		method:      "run",
	}

	cfg := &hostapi.EntryConfig{
		HostConfig: hostapi.Config{
			Workers:        workers,
			WorkerClass:    hostapi.WorkerClassWASM,
			QueueSize:      4096,
			LocalQueueSize: 512,
		},
	}

	pidGen := uniqid.NewPIDGenerator(uniqid.NewGenerator(), nodeID)
	host := servicehost.NewHost(
		hostID,
		cfg,
		scheduler,
		factory,
		pidGen,
		zap.NewNop(),
	)

	require.NoError(t, node.RegisterHost(hostPID, host))

	rootCtx := sqliteHarnessContext(t, clientReceiver)
	_, err := host.Start(rootCtx)
	require.NoError(t, err)

	cluster := &harnessCluster{
		node:      node,
		router:    router,
		host:      host,
		scheduler: scheduler,
		receiver:  clientReceiver,
		lifecycle: lifecycle,
		hostID:    hostID,
		hostPID:   hostPID,
		nodeID:    nodeID,
	}

	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, host.Stop(stopCtx))
	})

	return cluster
}

func (c *harnessCluster) SpawnActor(t testing.TB, sourceName string) pid.PID {
	t.Helper()
	rootCtx := sqliteHarnessContext(t, c.receiver)
	start := &processapi.Start{
		Source: registry.ParseID(sourceName),
		Options: attrs.NewBagFrom(map[string]any{
			processapi.ProcessNameKey: sourceName,
		}),
	}
	actorPID, err := c.host.Run(rootCtx, start)
	require.NoError(t, err)
	return actorPID
}

type harnessWorkerClient struct {
	cluster *harnessCluster
	pid     pid.PID
	replyCh chan *relayapi.Message
	uniqID  string
	closed  atomic.Bool
}

func (c *harnessCluster) NewClient(clientUniqID string) *harnessWorkerClient {
	clientPID := pid.PID{
		Node:   c.nodeID,
		Host:   "client",
		UniqID: clientUniqID,
	}
	ch := make(chan *relayapi.Message, 1024)
	c.receiver.registerClient(clientUniqID, ch)
	return &harnessWorkerClient{
		cluster: c,
		pid:     clientPID,
		replyCh: ch,
		uniqID:  clientUniqID,
	}
}

func (c *harnessWorkerClient) Close() {
	if c.closed.Swap(true) {
		return
	}
	c.cluster.receiver.unregisterClient(c.uniqID)
}

func (c *harnessWorkerClient) Request(ctx context.Context, target pid.PID, topic string, payloads ...payload.Payload) (*relayapi.Message, time.Duration, error) {
	start := time.Now()
	msg := relayapi.AcquireMessage()
	msg.Topic = topic
	if len(payloads) > 0 {
		msg.Payloads = payloads
	}
	pkg := relayapi.NewMessagePackage(c.pid, target, msg)

	if err := c.cluster.router.Send(pkg); err != nil {
		return nil, time.Since(start), fmt.Errorf("relay send failed: %w", err)
	}

	select {
	case reply := <-c.replyCh:
		elapsed := time.Since(start)
		return reply, elapsed, nil
	case <-ctx.Done():
		return nil, time.Since(start), ctx.Err()
	}
}

func (c *harnessWorkerClient) SendOnly(target pid.PID, topic string, payloads ...payload.Payload) error {
	msg := relayapi.AcquireMessage()
	msg.Topic = topic
	if len(payloads) > 0 {
		msg.Payloads = payloads
	}
	pkg := relayapi.NewMessagePackage(c.pid, target, msg)
	return c.cluster.router.Send(pkg)
}

// --- Process Step Gating Wrapper ---

type gatedProcess struct {
	processapi.Process
	gate      chan struct{}
	enterStep chan struct{}
	mu        sync.Mutex
}

func newGatedProcess(proc processapi.Process) *gatedProcess {
	return &gatedProcess{Process: proc}
}

func (g *gatedProcess) EventAdmission() processapi.EventAdmission {
	if ea, ok := g.Process.(interface {
		EventAdmission() processapi.EventAdmission
	}); ok {
		return ea.EventAdmission()
	}
	return nil
}

func (g *gatedProcess) ExecutionTimeout() time.Duration {
	if et, ok := g.Process.(interface{ ExecutionTimeout() time.Duration }); ok {
		return et.ExecutionTimeout()
	}
	return 0
}

func (g *gatedProcess) setGate(gate chan struct{}, enter chan struct{}) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.gate = gate
	g.enterStep = enter
}

func (g *gatedProcess) Step(events []processapi.Event, out *processapi.StepOutput) error {
	g.mu.Lock()
	enter := g.enterStep
	gate := g.gate
	g.mu.Unlock()

	if enter != nil {
		select {
		case enter <- struct{}{}:
		default:
		}
	}
	if gate != nil {
		<-gate
	}
	return g.Process.Step(events, out)
}

// --- Latency Reservoir (Bounded Memory) ---

// latencyReservoir collects uniform random samples (Algorithm R) to prevent
// harness memory from growing linearly with total operations.
type latencyReservoir struct {
	rng     *rand.Rand
	samples []time.Duration
	mu      sync.Mutex
	maxSize int
	count   uint64
}

func newLatencyReservoir(maxSize int, seed uint64) *latencyReservoir {
	return &latencyReservoir{
		samples: make([]time.Duration, 0, maxSize),
		maxSize: maxSize,
		rng:     rand.New(rand.NewPCG(seed, 42)),
	}
}

func (r *latencyReservoir) Add(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.count++
	if len(r.samples) < r.maxSize {
		r.samples = append(r.samples, d)
		return
	}
	j := r.rng.Uint64N(r.count)
	if j < uint64(r.maxSize) {
		r.samples[j] = d
	}
}

func (r *latencyReservoir) Percentiles() (p50, p95, p99 time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.samples) == 0 {
		return 0, 0, 0
	}
	sorted := make([]time.Duration, len(r.samples))
	copy(sorted, r.samples)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	n := len(sorted)
	return sorted[n*50/100], sorted[n*95/100], sorted[n*99/100]
}

// --- Protocol Parsers & Assertions ---

func extractPayloadText(msg *relayapi.Message) (string, error) {
	if msg == nil || len(msg.Payloads) == 0 {
		return "", errors.New("empty message payload")
	}
	data := msg.Payloads[0].Data()
	switch v := data.(type) {
	case []byte:
		if !utf8.Valid(v) {
			return "", errors.New("payload bytes are not valid UTF-8")
		}
		return string(v), nil
	case string:
		if !utf8.ValidString(v) {
			return "", errors.New("payload string is not valid UTF-8")
		}
		return v, nil
	default:
		return "", fmt.Errorf("unexpected payload data type: %T", data)
	}
}

func parseResultText(msg *relayapi.Message) (string, error) {
	if msg == nil {
		return "", errors.New("nil response message")
	}
	if msg.Topic == "error" {
		text, err := extractPayloadText(msg)
		if err != nil {
			return "", fmt.Errorf("guest error (unparseable payload): %w", err)
		}
		return "", fmt.Errorf("guest error: %s", text)
	}
	if msg.Topic != "result" {
		return "", fmt.Errorf("expected topic 'result', got %q", msg.Topic)
	}
	return extractPayloadText(msg)
}

func parseLoadReply(msg *relayapi.Message) (int64, error) {
	text, err := parseResultText(msg)
	if err != nil {
		return 0, err
	}
	if !strings.HasPrefix(text, "loaded:") {
		return 0, fmt.Errorf("unexpected load response text: %q", text)
	}
	nStr := strings.TrimPrefix(text, "loaded:")
	n, err := strconv.ParseInt(nStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid load count %q: %w", nStr, err)
	}
	return n, nil
}

func parseGetReply(msg *relayapi.Message) (int64, int64, error) {
	text, err := parseResultText(msg)
	if err != nil {
		return 0, 0, err
	}
	if !strings.HasPrefix(text, "value:") {
		return 0, 0, fmt.Errorf("unexpected get response text: %q", text)
	}
	parts := strings.Split(text, ":")
	if len(parts) != 3 {
		return 0, 0, fmt.Errorf("invalid value response format %q", text)
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid id %q: %w", parts[1], err)
	}
	val, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid value %q: %w", parts[2], err)
	}
	return id, val, nil
}

func parsePutReply(msg *relayapi.Message) (int64, int64, error) {
	text, err := parseResultText(msg)
	if err != nil {
		return 0, 0, err
	}
	if !strings.HasPrefix(text, "updated:") {
		return 0, 0, fmt.Errorf("unexpected put response text: %q", text)
	}
	parts := strings.Split(text, ":")
	if len(parts) != 3 {
		return 0, 0, fmt.Errorf("invalid updated response format %q", text)
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid id %q: %w", parts[1], err)
	}
	val, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid value %q: %w", parts[2], err)
	}
	return id, val, nil
}

func parseSumReply(msg *relayapi.Message) (int64, int64, error) {
	text, err := parseResultText(msg)
	if err != nil {
		return 0, 0, err
	}
	if !strings.HasPrefix(text, "sum:") {
		return 0, 0, fmt.Errorf("unexpected sum response text: %q", text)
	}
	parts := strings.Split(text, ":")
	if len(parts) != 3 {
		return 0, 0, fmt.Errorf("invalid sum response format %q", text)
	}
	count, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid sum count %q: %w", parts[1], err)
	}
	sumVal, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid sum value %q: %w", parts[2], err)
	}
	return count, sumVal, nil
}

type SQLiteStats struct {
	MemoryUsed      int64
	MemoryHighwater int64
	PageCount       int64
	PageSize        int64
}

func parseStatsReply(msg *relayapi.Message) (*SQLiteStats, error) {
	text, err := parseResultText(msg)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(text, "stats:") {
		return nil, fmt.Errorf("unexpected stats response text: %q", text)
	}
	parts := strings.Split(text, ":")
	if len(parts) != 5 {
		return nil, fmt.Errorf("invalid stats response format %q (expected 5 colon-separated fields)", text)
	}
	memUsed, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, err
	}
	memHigh, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return nil, err
	}
	pages, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return nil, err
	}
	pageSize, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		return nil, err
	}
	return &SQLiteStats{
		MemoryUsed:      memUsed,
		MemoryHighwater: memHigh,
		PageCount:       pages,
		PageSize:        pageSize,
	}, nil
}

// --- WASM Loading Helpers ---

func loadTestWASMBytes(t testing.TB, path string) ([]byte, bool) {
	if fixture := os.Getenv("WIPPY_SQLITE_FIXTURE"); fixture != "" && path == "testdata/sqlite_actor.wasm" {
		path = fixture
	}
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	return data, true
}

func createWASMActorProcess(
	ctx context.Context,
	wasmBytes []byte,
	memLimitBytes int64,
	actorLimits actorhost.Limits,
) (processapi.Process, error) {
	return createWASMActorProcessWithExecutionLimits(ctx, wasmBytes, memLimitBytes, actorLimits, wasmapi.LimitsConfig{})
}

func createWASMActorProcessWithExecutionLimits(
	ctx context.Context,
	wasmBytes []byte,
	memLimitBytes int64,
	actorLimits actorhost.Limits,
	executionLimits wasmapi.LimitsConfig,
) (processapi.Process, error) {
	return createWASMActorProcessWithCompilationCache(ctx, wasmBytes, memLimitBytes, actorLimits, executionLimits, nil)
}

func createSQLiteWASMActorProcess(
	ctx context.Context,
	wasmBytes []byte,
	memLimitBytes int64,
	actorLimits actorhost.Limits,
) (processapi.Process, error) {
	return createWASMActorProcessWithCompilationCache(ctx, wasmBytes, memLimitBytes, actorLimits, wasmapi.LimitsConfig{}, sqliteFixtureCompilationCache)
}

func createWASMActorProcessWithCompilationCache(
	ctx context.Context,
	wasmBytes []byte,
	memLimitBytes int64,
	actorLimits actorhost.Limits,
	executionLimits wasmapi.LimitsConfig,
	compilationCache wazero.CompilationCache,
) (processapi.Process, error) {
	rtCfg := &wasmrt.Config{
		CompilationCache:   compilationCache,
		CloseOnContextDone: true,
	}
	if memLimitBytes > 0 {
		rtCfg.MemoryLimitPages = uint32(memLimitBytes / wasmapi.MinProcessMemoryBytesMultiple)
	}
	rt, err := wasmrt.NewWithConfig(ctx, rtCfg)
	if err != nil {
		return nil, fmt.Errorf("wasmrt.NewWithConfig failed: %w", err)
	}

	resources := preview2.NewResourceTableWithLimits(128, 1)
	// These are the production WASI hosts needed by Rust's standard library.
	// The fixture uses SQLite's in-memory database; no filesystem preopens are granted.
	for _, h := range []wasmrt.Host{
		actorhost.NewHost(), iohost.NewErrorHost(resources), iohost.NewStreamsHost(resources),
		clihost.NewEnvironmentHost(), clihost.NewExitHost(),
		stdiohost.NewHost(resources), stdiohost.NewStdoutHost(resources), stdiohost.NewStderrHost(resources),
		stdiohost.NewTerminalStdinHost(), stdiohost.NewTerminalStdoutHost(), stdiohost.NewTerminalStderrHost(),
		clockhost.NewWallClockHost(), clockhost.NewMonotonicClockHost(resources),
		randhost.NewSecureRandomHost(), fshost.NewTypesHost(resources), fshost.NewPreopensHost(resources),
	} {
		if err := rt.RegisterHost(h); err != nil {
			_ = resources.Close()
			_ = rt.Close(ctx)
			return nil, fmt.Errorf("register host %s: %w", h.Namespace(), err)
		}
	}

	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		_ = rt.Close(ctx)
		_ = resources.Close()
		return nil, fmt.Errorf("load component failed: %w", err)
	}

	if err := mod.Compile(ctx); err != nil {
		_ = rt.Close(ctx)
		_ = resources.Close()
		return nil, fmt.Errorf("compile component failed: %w", err)
	}

	cleanup := func() {
		if err := resources.Close(); err != nil {
			panic(fmt.Sprintf("close SQLite resources: %v", err))
		}
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := rt.Close(closeCtx); err != nil {
			panic(fmt.Sprintf("close SQLite runtime: %v", err))
		}
	}

	proc := NewProcess(mod, "", wasmapi.WASIConfig{}, executionLimits, nil)
	actorProc := NewActorProcess(proc, actorLimits, cleanup)
	return actorProc, nil
}

// --- Tests ---

// TestSchedulerHarness_Infrastructure verifies that the load harness correctly routes
// messages, spawns actors, processes sends and replies, and handles lifecycle termination
// using the real scheduler host and relay without manually driving ActorProcess.Step.
// Uses existing testdata/actor.wasm.
func TestSchedulerHarness_Infrastructure(t *testing.T) {
	wasmBytes, ok := loadTestWASMBytes(t, "testdata/actor.wasm")
	if !ok {
		t.Fatal("testdata/actor.wasm must be present for infrastructure verification")
	}

	factoryFunc := func() (processapi.Process, error) {
		return createWASMActorProcess(context.Background(), wasmBytes, 0, actorhost.DefaultLimits())
	}

	cluster := newHarnessCluster(t, 2, factoryFunc)
	actorPID := cluster.SpawnActor(t, "infra-actor")
	doneCh := cluster.lifecycle.registerWait(actorPID)

	client := cluster.NewClient("test-client")
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Probe identity
	reply, dur, err := client.Request(ctx, actorPID, "probe")
	require.NoError(t, err)
	require.Equal(t, "identity", reply.Topic)
	require.Equal(t, actorPID.String(), string(reply.Payloads[0].Data().([]byte)))
	t.Logf("Infra probe round-trip via production scheduler/relay took %v", dur)

	// 2. Increment counters
	for i := uint64(1); i <= 5; i++ {
		reply, _, err := client.Request(ctx, actorPID, "increment")
		require.NoError(t, err)
		require.Equal(t, "count", reply.Topic)
		bytes := reply.Payloads[0].Data().([]byte)
		val := binary.LittleEndian.Uint64(bytes)
		require.Equal(t, i, val, "counter state retention across scheduler worker steps")
	}

	// 3. Stop actor (guest exits cleanly, scheduler completes processor with res.Error == nil)
	require.NoError(t, client.SendOnly(actorPID, "stop"))
	select {
	case res := <-doneCh:
		require.NotNil(t, res)
		require.NoError(t, res.Error, "graceful stop must complete with nil error")
		t.Logf("Actor terminated cleanly in scheduler: err=%v", res.Error)
	case <-time.After(5 * time.Second):
		t.Fatal("Actor did not terminate within timeout after stop")
	}
}

// TestSQLiteActor_Correctness_1Actor verifies 1 actor retained SQLite DB with 10,000 rows.
func TestSQLiteActor_Correctness_1Actor(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy WASM workload; run make test-wasm-heavy locally")
	}
	wasmBytes, ok := loadTestWASMBytes(t, "testdata/sqlite_actor.wasm")
	if !ok {
		t.Fatal("required SQLite fixture is unavailable")
	}

	factoryFunc := func() (processapi.Process, error) {
		return createSQLiteWASMActorProcess(context.Background(), wasmBytes, 0, actorhost.DefaultLimits())
	}

	cluster := newHarnessCluster(t, 2, factoryFunc)
	actorPID := cluster.SpawnActor(t, "sqlite-single-actor")
	doneCh := cluster.lifecycle.registerWait(actorPID)

	client := cluster.NewClient("single-client")
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Load 10,000 rows (ids 0..9999, val = id*3)
	const nRows = int64(10000)
	loadReply, dur, err := client.Request(ctx, actorPID, fmt.Sprintf("load:%d", nRows))
	require.NoError(t, err)
	loadedN, err := parseLoadReply(loadReply)
	require.NoError(t, err)
	require.Equal(t, nRows, loadedN)
	t.Logf("load:%d completed in %v", nRows, dur)

	// 2. Verify Initial Sum Oracle: sum(0..N-1 of id*3) = 3 * (N-1)*N / 2
	expectedSum := int64(3) * (nRows - 1) * nRows / 2
	sumReply, _, err := client.Request(ctx, actorPID, "sum")
	require.NoError(t, err)
	count, sumVal, err := parseSumReply(sumReply)
	require.NoError(t, err)
	require.Equal(t, nRows, count, "initial row count")
	require.Equal(t, expectedSum, sumVal, "initial sum value")

	// 3. Perform verified gets
	for _, id := range []int64{0, 1, 42, 999, 5000, 9999} {
		getReply, _, err := client.Request(ctx, actorPID, fmt.Sprintf("get:%d", id))
		require.NoError(t, err)
		gotID, gotVal, err := parseGetReply(getReply)
		require.NoError(t, err)
		require.Equal(t, id, gotID)
		require.Equal(t, id*3, gotVal, "initial value mismatch for id %d", id)
	}

	// 4. Perform puts and track oracle sum only after successful ACK
	updates := map[int64]int64{
		10:   77777,
		100:  99999,
		5000: 123456,
	}
	for id, newVal := range updates {
		oldVal := id * 3
		putReply, _, err := client.Request(ctx, actorPID, fmt.Sprintf("put:%d:%d", id, newVal))
		require.NoError(t, err)
		upID, upVal, err := parsePutReply(putReply)
		require.NoError(t, err)
		require.Equal(t, id, upID)
		require.Equal(t, newVal, upVal)
		expectedSum = expectedSum - oldVal + newVal

		// Verify subsequent get
		getReply, _, err := client.Request(ctx, actorPID, fmt.Sprintf("get:%d", id))
		require.NoError(t, err)
		gID, gVal, err := parseGetReply(getReply)
		require.NoError(t, err)
		require.Equal(t, id, gID)
		require.Equal(t, newVal, gVal)
	}

	// 5. Verify Updated Sum Oracle
	sumReply2, _, err := client.Request(ctx, actorPID, "sum")
	require.NoError(t, err)
	count2, sumVal2, err := parseSumReply(sumReply2)
	require.NoError(t, err)
	require.Equal(t, nRows, count2)
	require.Equal(t, expectedSum, sumVal2, "sum oracle after updates")

	// 6. Inspect SQLite Memory Stats
	statsReply, _, err := client.Request(ctx, actorPID, "stats")
	require.NoError(t, err)
	stats, err := parseStatsReply(statsReply)
	require.NoError(t, err)
	require.Greater(t, stats.MemoryUsed, int64(0))
	require.Greater(t, stats.PageCount, int64(0))

	// 7. Stop actor gracefully
	require.NoError(t, client.SendOnly(actorPID, "stop"))
	select {
	case res := <-doneCh:
		require.NotNil(t, res)
		require.NoError(t, res.Error, "graceful stop must complete with nil error")
	case <-time.After(5 * time.Second):
		t.Fatal("Actor failed to stop in scheduler")
	}
}

// TestSQLiteActor_Correctness_4Actors_Concurrent verifies 4 concurrent actors each
// retaining an independent SQLite DB of 10,000 rows.
func TestSQLiteActor_Correctness_4Actors_Concurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy WASM workload; run make test-wasm-heavy locally")
	}
	wasmBytes, ok := loadTestWASMBytes(t, "testdata/sqlite_actor.wasm")
	if !ok {
		t.Fatal("required SQLite fixture is unavailable")
	}

	const numActors = 4
	const nRows = int64(10000)

	factoryFunc := func() (processapi.Process, error) {
		return createSQLiteWASMActorProcess(context.Background(), wasmBytes, 0, actorhost.DefaultLimits())
	}

	cluster := newHarnessCluster(t, numActors, factoryFunc)

	type actorState struct {
		client      *harnessWorkerClient
		doneCh      chan *runtimeapi.Result
		updates     map[int64]int64
		pid         pid.PID
		expectedSum int64
	}

	actors := make([]*actorState, numActors)
	for i := 0; i < numActors; i++ {
		actorPID := cluster.SpawnActor(t, fmt.Sprintf("sqlite-actor-%d", i))
		client := cluster.NewClient(fmt.Sprintf("client-%d", i))
		doneCh := cluster.lifecycle.registerWait(actorPID)
		actors[i] = &actorState{
			pid:         actorPID,
			client:      client,
			doneCh:      doneCh,
			expectedSum: int64(3) * (nRows - 1) * nRows / 2,
			updates:     make(map[int64]int64),
		}
	}
	defer func() {
		for _, a := range actors {
			a.client.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1. Concurrently load all 4 actors
	var wg sync.WaitGroup
	errCh := make(chan error, numActors*10)

	for i := 0; i < numActors; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			a := actors[idx]
			reply, _, err := a.client.Request(ctx, a.pid, fmt.Sprintf("load:%d", nRows))
			if err != nil {
				errCh <- fmt.Errorf("actor %d load error: %w", idx, err)
				return
			}
			loadedN, err := parseLoadReply(reply)
			if err != nil || loadedN != nRows {
				errCh <- fmt.Errorf("actor %d bad load reply: %w", idx, err)
				return
			}
		}(i)
	}
	wg.Wait()
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}

	// 2. Concurrently run mixed gets and puts per actor with independent update targets
	for i := 0; i < numActors; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			a := actors[idx]
			for step := int64(1); step <= 50; step++ {
				id := (step*17 + int64(idx)*100) % nRows
				newVal := step*1000 + int64(idx)
				oldVal := id * 3
				if existing, ok := a.updates[id]; ok {
					oldVal = existing
				}

				putReply, _, err := a.client.Request(ctx, a.pid, fmt.Sprintf("put:%d:%d", id, newVal))
				if err != nil {
					errCh <- fmt.Errorf("actor %d put error: %w", idx, err)
					return
				}
				upID, val, err := parsePutReply(putReply)
				if err != nil || upID != id || val != newVal {
					errCh <- fmt.Errorf("actor %d bad put reply: %w", idx, err)
					return
				}
				// Apply oracle only after ACK
				a.updates[id] = newVal
				a.expectedSum = a.expectedSum - oldVal + newVal

				// Spot-check get
				getReply, _, err := a.client.Request(ctx, a.pid, fmt.Sprintf("get:%d", id))
				if err != nil {
					errCh <- fmt.Errorf("actor %d get error: %w", idx, err)
					return
				}
				gID, gVal, err := parseGetReply(getReply)
				if err != nil || gID != id || gVal != newVal {
					errCh <- fmt.Errorf("actor %d bad get reply: %w", idx, err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}

	// 3. Verify Sum Oracle for each actor
	for idx, a := range actors {
		sumReply, _, err := a.client.Request(ctx, a.pid, "sum")
		require.NoError(t, err)
		count, sumVal, err := parseSumReply(sumReply)
		require.NoError(t, err)
		require.Equal(t, nRows, count, "actor %d count", idx)
		require.Equal(t, a.expectedSum, sumVal, "actor %d sum oracle", idx)
	}

	// 4. Stop all actors
	for idx, a := range actors {
		require.NoError(t, a.client.SendOnly(a.pid, "stop"))
		select {
		case res := <-a.doneCh:
			require.NotNil(t, res)
			require.NoError(t, res.Error, "actor %d graceful stop must complete with nil error", idx)
		case <-time.After(5 * time.Second):
			t.Fatalf("Actor %d failed to stop", idx)
		}
	}
}

// TestSQLiteActorLoad_Sustained is an opt-in heavy load test run with WIPPY_SQLITE_LOAD=1.
// Supports configurable parameters via environment variables with bounds validation:
// - WIPPY_SQLITE_LOAD_ROWS (1..1000000, default 100000)
// - WIPPY_SQLITE_LOAD_DURATION (<=5m, default 30s)
// - WIPPY_SQLITE_LOAD_CONCURRENCY (1..64, default 4)
// - WIPPY_SQLITE_LOAD_ACTORS (1..64, default 4)
func TestSQLiteActorLoad_Sustained(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy WASM workload; run make test-wasm-heavy locally")
	}
	if os.Getenv("WIPPY_SQLITE_LOAD") == "" {
		t.Skip("skipping heavy sustained load test; set WIPPY_SQLITE_LOAD=1 to run")
	}

	wasmBytes, ok := loadTestWASMBytes(t, "testdata/sqlite_actor.wasm")
	if !ok {
		t.Fatal("testdata/sqlite_actor.wasm absent: required for opt-in sustained load test")
	}

	// Parameter extraction and bounds validation
	nRows := int64(100000)
	if envRows := os.Getenv("WIPPY_SQLITE_LOAD_ROWS"); envRows != "" {
		val, err := strconv.ParseInt(envRows, 10, 64)
		require.NoError(t, err, "parse WIPPY_SQLITE_LOAD_ROWS")
		nRows = val
	}
	require.True(t, nRows > 0 && nRows <= 1_000_000, "WIPPY_SQLITE_LOAD_ROWS must be in range 1..1000000")

	duration := 30 * time.Second
	if envDur := os.Getenv("WIPPY_SQLITE_LOAD_DURATION"); envDur != "" {
		d, err := time.ParseDuration(envDur)
		require.NoError(t, err, "parse WIPPY_SQLITE_LOAD_DURATION")
		duration = d
	}
	require.True(t, duration > 0 && duration <= 5*time.Minute, "WIPPY_SQLITE_LOAD_DURATION must be >0 and <=5m")

	concurrency := 4
	if envConc := os.Getenv("WIPPY_SQLITE_LOAD_CONCURRENCY"); envConc != "" {
		c, err := strconv.Atoi(envConc)
		require.NoError(t, err, "parse WIPPY_SQLITE_LOAD_CONCURRENCY")
		concurrency = c
	}
	require.True(t, concurrency > 0 && concurrency <= 64, "WIPPY_SQLITE_LOAD_CONCURRENCY must be in range 1..64")

	numActors := 4
	if envAct := os.Getenv("WIPPY_SQLITE_LOAD_ACTORS"); envAct != "" {
		a, err := strconv.Atoi(envAct)
		require.NoError(t, err, "parse WIPPY_SQLITE_LOAD_ACTORS")
		numActors = a
	}
	require.True(t, numActors > 0 && numActors <= 64, "WIPPY_SQLITE_LOAD_ACTORS must be in range 1..64")

	t.Logf("=== Sustained SQLite WASM Load Test Configuration ===")
	t.Logf("Rows per actor: %d, Duration: %v, Concurrency: %d workers, Actors: %d",
		nRows, duration, concurrency, numActors)

	// Memory Snapshot: Baseline
	runtime.GC()
	var mBaseline runtime.MemStats
	runtime.ReadMemStats(&mBaseline)

	factoryFunc := func() (processapi.Process, error) {
		return createSQLiteWASMActorProcess(context.Background(), wasmBytes, 0, actorhost.DefaultLimits())
	}

	cluster := newHarnessCluster(t, concurrency, factoryFunc)

	type actorState struct {
		doneCh chan *runtimeapi.Result
		pid    pid.PID
	}

	actors := make([]*actorState, numActors)
	for i := 0; i < numActors; i++ {
		actorPID := cluster.SpawnActor(t, fmt.Sprintf("load-actor-%d", i))
		doneCh := cluster.lifecycle.registerWait(actorPID)
		actors[i] = &actorState{
			pid:    actorPID,
			doneCh: doneCh,
		}
	}

	// Pre-load data in all actors using bounded context
	initClient := cluster.NewClient("init-client")
	for idx, a := range actors {
		initCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		loadReply, dur, err := initClient.Request(initCtx, a.pid, fmt.Sprintf("load:%d", nRows))
		cancel()
		require.NoError(t, err)
		loadedN, err := parseLoadReply(loadReply)
		require.NoError(t, err)
		require.Equal(t, nRows, loadedN)
		t.Logf("Actor %d load completed in %v", idx, dur)
	}
	initClient.Close()

	// Memory Snapshot: After DB Load
	runtime.GC()
	var mLoaded runtime.MemStats
	runtime.ReadMemStats(&mLoaded)
	t.Logf("Memory after loading %d rows across %d actors: HeapAlloc=%d KB, HeapInuse=%d KB, Sys=%d KB",
		nRows*int64(numActors), numActors, mLoaded.HeapAlloc/1024, mLoaded.HeapInuse/1024, mLoaded.Sys/1024)

	var totalOps atomic.Uint64
	var errorCount atomic.Uint64

	// Bounded latency collection via reservoir sampling (20,000 samples)
	reservoir := newLatencyReservoir(20000, 9999)

	var peakHeapAlloc atomic.Uint64
	stopMemMonitor := make(chan struct{})
	memMonitorDone := make(chan struct{})
	go func() {
		defer close(memMonitorDone)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				for {
					currentPeak := peakHeapAlloc.Load()
					if m.HeapAlloc <= currentPeak || peakHeapAlloc.CompareAndSwap(currentPeak, m.HeapAlloc) {
						break
					}
				}
			case <-stopMemMonitor:
				return
			}
		}
	}()

	startLoad := time.Now()
	stopOfferingAt := startLoad.Add(duration)
	var loadWg sync.WaitGroup

	// Per-worker state: worker owns its partition of row IDs (id % concurrency == workerID).
	// Maps: actorIdx -> (id -> acknowledged value).
	type workerPartitionState struct {
		values []map[int64]int64
	}
	workerStates := make([]*workerPartitionState, concurrency)
	for w := 0; w < concurrency; w++ {
		st := &workerPartitionState{values: make([]map[int64]int64, numActors)}
		for a := 0; a < numActors; a++ {
			st.values[a] = make(map[int64]int64)
		}
		workerStates[w] = st
	}

	for workerID := 0; workerID < concurrency; workerID++ {
		loadWg.Add(1)
		go func(wID int) {
			defer loadWg.Done()
			client := cluster.NewClient(fmt.Sprintf("load-worker-%d", wID))
			defer client.Close()

			// Fast local deterministic PCG PRNG (no big.Int or crypto/rand)
			rng := rand.New(rand.NewPCG(uint64(wID+1)*10007, 777))
			opCounter := 0

			// Compute number of rows in this worker's partition: id % concurrency == wID
			numPartitionRows := (nRows - int64(wID) + int64(concurrency) - 1) / int64(concurrency)
			if numPartitionRows <= 0 {
				return
			}

			wState := workerStates[wID]

			for time.Now().Before(stopOfferingAt) {
				actorIdx := (wID + opCounter) % numActors
				targetActor := actors[actorIdx]
				opCounter++

				// Select a row ID belonging strictly to this worker's partition
				slot := rng.Int64N(numPartitionRows)
				id := slot*int64(concurrency) + int64(wID)

				// Determine expected value: initial id*3 or last acknowledged write
				expectedVal := id * 3
				if lastVal, ok := wState.values[actorIdx][id]; ok {
					expectedVal = lastVal
				}

				// 80% GET, 20% PUT
				isPut := rng.IntN(100) < 20

				// Each accepted request uses a separate bounded timeout
				reqCtx, reqCancel := context.WithTimeout(context.Background(), 5*time.Second)

				if isPut {
					newVal := rng.Int64N(1_000_000) + 1
					reply, dur, err := client.Request(reqCtx, targetActor.pid, fmt.Sprintf("put:%d:%d", id, newVal))
					reqCancel()
					if err != nil {
						errorCount.Add(1)
						continue
					}
					upID, upVal, err := parsePutReply(reply)
					if err != nil || upID != id || upVal != newVal {
						errorCount.Add(1)
						continue
					}
					// Apply oracle update ONLY AFTER successful ACK
					wState.values[actorIdx][id] = newVal
					reservoir.Add(dur)
					totalOps.Add(1)
				} else {
					reply, dur, err := client.Request(reqCtx, targetActor.pid, fmt.Sprintf("get:%d", id))
					reqCancel()
					if err != nil {
						errorCount.Add(1)
						continue
					}
					gotID, gotVal, err := parseGetReply(reply)
					if err != nil || gotID != id || gotVal != expectedVal {
						errorCount.Add(1)
						continue
					}
					reservoir.Add(dur)
					totalOps.Add(1)
				}
			}
		}(workerID)
	}

	loadWg.Wait()
	close(stopMemMonitor)
	<-memMonitorDone // joined memory monitor
	actualDuration := time.Since(startLoad)

	// Memory Snapshot: End of Load
	var mEnd runtime.MemStats
	runtime.ReadMemStats(&mEnd)

	// Validate Final Sum Oracle for each actor after complete drain
	oracleClient := cluster.NewClient("oracle-client")
	defer oracleClient.Close()

	for aIdx, a := range actors {
		// Calculate exact expected sum across all worker partitions
		// Initial sum: 3 * (N-1)*N / 2
		initialSum := int64(3) * (nRows - 1) * nRows / 2
		var totalDelta int64
		for w := 0; w < concurrency; w++ {
			for id, finalVal := range workerStates[w].values[aIdx] {
				totalDelta += (finalVal - id*3)
			}
		}
		expectedFinalSum := initialSum + totalDelta

		sumCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		sumReply, _, err := oracleClient.Request(sumCtx, a.pid, "sum")
		cancel()
		require.NoError(t, err)
		count, sumVal, err := parseSumReply(sumReply)
		require.NoError(t, err)
		require.Equal(t, nRows, count, "final row count for actor %d", aIdx)
		require.Equal(t, expectedFinalSum, sumVal, "final sum oracle match for actor %d", aIdx)

		statsCtx, cancelStats := context.WithTimeout(context.Background(), 10*time.Second)
		statsReply, _, err := oracleClient.Request(statsCtx, a.pid, "stats")
		cancelStats()
		require.NoError(t, err)
		stats, err := parseStatsReply(statsReply)
		require.NoError(t, err)
		t.Logf("Actor %d SQLite internal stats: mem_used=%d KB, mem_highwater=%d KB, pages=%d, page_size=%d",
			aIdx, stats.MemoryUsed/1024, stats.MemoryHighwater/1024, stats.PageCount, stats.PageSize)
	}

	// Stop all actors gracefully with bounded timeout
	for idx, a := range actors {
		require.NoError(t, oracleClient.SendOnly(a.pid, "stop"))
		select {
		case res := <-a.doneCh:
			require.NotNil(t, res)
			require.NoError(t, res.Error, "actor %d graceful stop must complete with nil error", idx)
		case <-time.After(5 * time.Second):
			t.Fatalf("Actor %d failed to stop cleanly", idx)
		}
	}

	// Teardown cluster with bounded timeout
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 10*time.Second)
	require.NoError(t, cluster.host.Stop(stopCtx))
	cancelStop()

	// Memory Snapshot: After Full Cleanup
	runtime.GC()
	var mCleanup runtime.MemStats
	runtime.ReadMemStats(&mCleanup)

	p50, p95, p99 := reservoir.Percentiles()
	opsPerSec := float64(totalOps.Load()) / actualDuration.Seconds()

	t.Logf("================ Sustained Load Test Results ================")
	t.Logf("Total Duration:      %v", actualDuration)
	t.Logf("Completed Requests:  %d", totalOps.Load())
	t.Logf("Throughput:          %.2f ops/sec", opsPerSec)
	t.Logf("Error Count:         %d", errorCount.Load())
	t.Logf("End-to-End Latency:  p50=%v, p95=%v, p99=%v (from %d reservoir samples of %d observations)", p50, p95, p99, len(reservoir.samples), reservoir.count)
	t.Logf("Memory Baseline:     HeapAlloc=%d KB, Sys=%d KB", mBaseline.HeapAlloc/1024, mBaseline.Sys/1024)
	t.Logf("Memory Peak:         HeapAlloc=%d KB", peakHeapAlloc.Load()/1024)
	t.Logf("Memory End:          HeapAlloc=%d KB, Sys=%d KB", mEnd.HeapAlloc/1024, mEnd.Sys/1024)
	t.Logf("Memory Post-Cleanup: HeapAlloc=%d KB, Sys=%d KB (GC Runs: %d)", mCleanup.HeapAlloc/1024, mCleanup.Sys/1024, mCleanup.NumGC-mBaseline.NumGC)
	t.Logf("NOTE: Don't claim zero leak from single RSS; Go GC retains virtual pages in Sys.")
	t.Logf("=============================================================")

	require.Greater(t, totalOps.Load(), uint64(0), "must complete non-zero requests")
	require.Zero(t, errorCount.Load(), "no errors permitted during sustained load")
	require.Zero(t, cluster.receiver.dropped.Load(), "no dropped replies permitted")
}

// TestSQLiteActor_MemoryLimits forces an actual oversized dataset exceeding a tight memory limit
// and asserts guest failure/termination + bounded guest memory, followed by proving a healthy actor works.
func TestSQLiteActor_MemoryLimits(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy WASM workload; run make test-wasm-heavy locally")
	}
	wasmBytes, ok := loadTestWASMBytes(t, "testdata/sqlite_actor.wasm")
	if !ok {
		t.Fatal("required SQLite fixture is unavailable")
	}

	// 2 MB memory limit (32 WASM pages)
	const tightLimit = int64(2 * 1024 * 1024)
	factoryFunc := func() (processapi.Process, error) {
		return createSQLiteWASMActorProcess(context.Background(), wasmBytes, tightLimit, actorhost.DefaultLimits())
	}

	cluster := newHarnessCluster(t, 2, factoryFunc)
	actorPID := cluster.SpawnActor(t, "memlimit-actor")
	doneCh := cluster.lifecycle.registerWait(actorPID)

	client := cluster.NewClient("memlimit-client")
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Force actual oversized load: 200,000 rows cannot fit in 2 MB (requires >15 MB)
	reply, _, reqErr := client.Request(ctx, actorPID, "load:200000")

	// A transport timeout or arbitrary guest error is not evidence of OOM.
	require.NoError(t, reqErr, "memory exhaustion must return an explicit guest response")
	require.NotNil(t, reply)
	require.Equal(t, "error", reply.Topic)
	errorText, err := extractPayloadText(reply)
	require.NoError(t, err)
	require.Contains(t, errorText, "out of memory", "expected SQLite allocation failure")
	t.Logf("Explicit guest allocation failure: %s", errorText)

	// An allocation failure must leave the actor available for later requests.
	recoveryReply, _, err := client.Request(ctx, actorPID, "load:100")
	require.NoError(t, err)
	recoveryN, err := parseLoadReply(recoveryReply)
	require.NoError(t, err)
	require.Equal(t, int64(100), recoveryN)
	require.NoError(t, client.SendOnly(actorPID, "stop"))
	select {
	case res := <-doneCh:
		require.NotNil(t, res)
		require.NoError(t, res.Error)
	case <-time.After(5 * time.Second):
		t.Fatal("memory-limited actor failed to stop after recovery")
	}

	// 2. Verify a healthy actor with sufficient memory still functions normally
	const healthyLimit = int64(32 * 1024 * 1024) // 32 MB
	healthyFactory := func() (processapi.Process, error) {
		return createSQLiteWASMActorProcess(context.Background(), wasmBytes, healthyLimit, actorhost.DefaultLimits())
	}
	healthyCluster := newHarnessCluster(t, 2, healthyFactory)
	healthyPID := healthyCluster.SpawnActor(t, "healthy-mem-actor")
	healthyDone := healthyCluster.lifecycle.registerWait(healthyPID)
	healthyClient := healthyCluster.NewClient("healthy-client")
	defer healthyClient.Close()

	hCtx, hCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer hCancel()

	hReply, _, err := healthyClient.Request(hCtx, healthyPID, "load:100")
	require.NoError(t, err)
	n, err := parseLoadReply(hReply)
	require.NoError(t, err)
	require.Equal(t, int64(100), n)

	getReply, _, err := healthyClient.Request(hCtx, healthyPID, "get:0")
	require.NoError(t, err)
	id, val, err := parseGetReply(getReply)
	require.NoError(t, err)
	require.Equal(t, int64(0), id)
	require.Equal(t, int64(0), val)

	require.NoError(t, healthyClient.SendOnly(healthyPID, "stop"))
	select {
	case res := <-healthyDone:
		require.NotNil(t, res)
		require.NoError(t, res.Error, "healthy actor graceful stop must complete with nil error")
	case <-time.After(5 * time.Second):
		t.Fatal("healthy actor failed to stop")
	}
}

// TestSQLiteActor_LifecycleAndCancellation verifies repeated lifecycles (graceful exits)
// and mid-execution cancellation (proving guest entered active compute before termination).
// sqliteInsertListener signals after a successful B-tree insertion. It observes
// guest execution without blocking it or changing the fixture's protocol.
type sqliteInsertListener struct {
	experimental.FunctionListener
	completed chan struct{}
	armed     atomic.Bool
}

func (l *sqliteInsertListener) After(_ context.Context, _ wazeroapi.Module, _ wazeroapi.FunctionDefinition, results []uint64) {
	if l.armed.Load() && len(results) == 1 && results[0] == 0 {
		select {
		case l.completed <- struct{}{}:
		default:
		}
	}
}

func TestSQLiteActor_LifecycleAndCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy WASM workload; run make test-wasm-heavy locally")
	}
	wasmBytes, ok := loadTestWASMBytes(t, "testdata/sqlite_actor.wasm")
	if !ok {
		t.Fatal("required SQLite fixture is unavailable")
	}

	listener := &sqliteInsertListener{
		FunctionListener: experimental.FunctionListenerFunc(func(context.Context, wazeroapi.Module, wazeroapi.FunctionDefinition, []uint64, experimental.StackIterator) {
		}),
		completed: make(chan struct{}, 1),
	}
	compileCtx := experimental.WithFunctionListenerFactory(context.Background(), experimental.FunctionListenerFactoryFunc(func(def wazeroapi.FunctionDefinition) experimental.FunctionListener {
		if def.Name() == "sqlite3BtreeInsert" {
			return listener
		}
		return nil
	}))
	factoryFunc := func() (processapi.Process, error) {
		return createSQLiteWASMActorProcess(compileCtx, wasmBytes, 0, actorhost.DefaultLimits())
	}

	cluster := newHarnessCluster(t, 2, factoryFunc)
	client := cluster.NewClient("lifecycle-client")
	defer client.Close()

	// 1. Repeated lifecycle: spawn, load, stop, verify res.Error == nil
	for cycle := 1; cycle <= 3; cycle++ {
		actorPID := cluster.SpawnActor(t, fmt.Sprintf("cycle-actor-%d", cycle))
		doneCh := cluster.lifecycle.registerWait(actorPID)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		reply, _, err := client.Request(ctx, actorPID, "load:500")
		cancel()
		require.NoError(t, err)
		n, err := parseLoadReply(reply)
		require.NoError(t, err)
		require.Equal(t, int64(500), n)

		require.NoError(t, client.SendOnly(actorPID, "stop"))
		select {
		case res := <-doneCh:
			require.NotNil(t, res)
			require.NoError(t, res.Error, "cycle %d graceful stop must have nil error", cycle)
		case <-time.After(5 * time.Second):
			t.Fatalf("Cycle %d actor did not stop", cycle)
		}
	}

	// 2. Cancellation: Prove guest entered active compute before termination
	termPID := cluster.SpawnActor(t, "cancel-target-actor")
	doneCh := cluster.lifecycle.registerWait(termPID)

	listener.armed.Store(true)
	// A long load gives cancellation an active computation to interrupt.
	require.NoError(t, client.SendOnly(termPID, "load:1000000"))
	select {
	case <-listener.completed:
		t.Log("Verified successful guest B-tree insertion before termination")
	case <-time.After(5 * time.Second):
		t.Fatal("No successful guest insertion observed before cancellation")
	}
	select {
	case res := <-doneCh:
		t.Fatalf("actor exited before cancellation: %v", res)
	default:
	}

	termCtx, termCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer termCancel()
	require.NoError(t, cluster.host.Terminate(termCtx, termPID))

	select {
	case res := <-doneCh:
		require.NotNil(t, res)
		require.Error(t, res.Error, "terminated mid-execution actor must complete with non-nil error")
		t.Logf("Actor terminated mid-execution cleanly: %v", res.Error)
	case <-time.After(5 * time.Second):
		t.Fatal("Terminated actor did not exit in scheduler")
	}
}

// TestSQLiteActor_Overload verifies typed mailbox rejection under overload,
// deterministically holding guest execution during burst, accounting accepted/rejected,
// draining replies, and verifying recovery.
func TestSQLiteActor_Overload(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy WASM workload; run make test-wasm-heavy locally")
	}
	wasmBytes, ok := loadTestWASMBytes(t, "testdata/sqlite_actor.wasm")
	if !ok {
		t.Fatal("required SQLite fixture is unavailable")
	}

	// Tiny mailbox capacity: 4 messages
	tightMailbox := actorhost.Limits{
		Capacity:     4,
		Bytes:        4096,
		MessageBytes: 1024,
	}

	var activeGated *gatedProcess
	var activeGatedMu sync.Mutex

	factoryFunc := func() (processapi.Process, error) {
		p, err := createSQLiteWASMActorProcess(context.Background(), wasmBytes, 0, tightMailbox)
		if err != nil {
			return nil, err
		}
		gp := newGatedProcess(p)
		activeGatedMu.Lock()
		activeGated = gp
		activeGatedMu.Unlock()
		return gp, nil
	}

	cluster := newHarnessCluster(t, 1, factoryFunc)
	actorPID := cluster.SpawnActor(t, "overload-actor")
	doneCh := cluster.lifecycle.registerWait(actorPID)

	activeGatedMu.Lock()
	gp := activeGated
	activeGatedMu.Unlock()
	require.NotNil(t, gp)

	client := cluster.NewClient("overload-client")
	defer client.Close()

	// 1. Initial setup: load small dataset
	initCtx, initCancel := context.WithTimeout(context.Background(), 5*time.Second)
	loadReply, _, err := client.Request(initCtx, actorPID, "load:10")
	initCancel()
	require.NoError(t, err)
	_, err = parseLoadReply(loadReply)
	require.NoError(t, err)

	// 2. Set deterministic gate on process wrapper
	stepGate := make(chan struct{})
	enterStep := make(chan struct{}, 1)
	gp.setGate(stepGate, enterStep)
	defer func() {
		gp.setGate(nil, nil)
		select {
		case <-stepGate:
		default:
			close(stepGate)
		}
	}()

	// 3. Send initial message to engage the gate in Step
	require.NoError(t, client.SendOnly(actorPID, "get:0"))
	select {
	case <-enterStep:
		t.Log("Scheduler worker deterministically gated inside guest Step")
	case <-time.After(5 * time.Second):
		t.Fatal("Worker failed to enter gated Step")
	}

	// 4. Send burst of messages exceeding capacity 4 through real host/router
	var accepted, rejected int
	for i := 1; i <= 15; i++ {
		sendErr := client.SendOnly(actorPID, fmt.Sprintf("get:%d", i%10))
		if sendErr != nil {
			require.True(t, errors.Is(sendErr, actorhost.ErrOverloaded),
				"overload error must be typed ErrOverloaded, got: %v", sendErr)
			rejected++
		} else {
			accepted++
		}
	}
	t.Logf("Overload burst completed: accepted=%d (within mailbox), rejected=%d (typed ErrOverloaded)",
		accepted, rejected)
	require.Greater(t, rejected, 0, "must reject messages when mailbox capacity is exceeded")
	require.Greater(t, accepted, 0, "must accept messages up to mailbox capacity")

	// 5. Release gate and drain all accepted replies
	close(stepGate)
	// Total accepted messages to drain: 1 (initial gating message) + accepted
	totalToDrain := 1 + accepted
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer drainCancel()

	drained := 0
	for drained < totalToDrain {
		select {
		case reply := <-client.replyCh:
			require.Equal(t, "result", reply.Topic)
			drained++
		case <-drainCtx.Done():
			t.Fatalf("Timeout draining replies: drained %d of %d", drained, totalToDrain)
		}
	}
	t.Logf("Successfully drained %d replies from admitted messages", drained)

	// 6. Verify actor recovers and handles subsequent requests normally
	recovCtx, recovCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer recovCancel()
	recovReply, _, err := client.Request(recovCtx, actorPID, "get:0")
	require.NoError(t, err)
	gotID, gotVal, err := parseGetReply(recovReply)
	require.NoError(t, err)
	require.Equal(t, int64(0), gotID)
	require.Equal(t, int64(0), gotVal)
	t.Log("Actor successfully recovered after overload")

	// 7. Clean stop
	require.NoError(t, client.SendOnly(actorPID, "stop"))
	select {
	case res := <-doneCh:
		require.NotNil(t, res)
		require.NoError(t, res.Error, "graceful stop after overload recovery must have nil error")
	case <-time.After(5 * time.Second):
		t.Fatal("Actor failed to stop after overload recovery")
	}
}
