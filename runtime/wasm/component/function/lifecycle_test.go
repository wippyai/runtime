// SPDX-License-Identifier: MPL-2.0

package function

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/api/topology"
	systemrelay "github.com/wippyai/runtime/system/relay"
	funcpool "github.com/wippyai/runtime/system/scheduler/pool"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

type lifecycleTestBus struct {
	onSend func()
	events []event.Event
}

func (b *lifecycleTestBus) Subscribe(context.Context, event.System, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}
func (b *lifecycleTestBus) SubscribeP(context.Context, event.System, event.Kind, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}
func (b *lifecycleTestBus) Unsubscribe(context.Context, event.SubscriberID) {}
func (b *lifecycleTestBus) Send(_ context.Context, evt event.Event) {
	if b.onSend != nil {
		b.onSend()
	}
	b.events = append(b.events, evt)
}

type lifecycleTestAwaitService struct {
	result   event.AwaitResult
	prepared bool
}

type lifecycleTestAwaitWaiter struct {
	result event.AwaitResult
}

func (w *lifecycleTestAwaitWaiter) Wait() event.AwaitResult { return w.result }
func (w *lifecycleTestAwaitWaiter) Close()                  {}

func (a *lifecycleTestAwaitService) Prepare(context.Context, event.System, event.Kind, event.Path, time.Duration) (event.AwaitWaiter, error) {
	a.prepared = true
	return &lifecycleTestAwaitWaiter{result: a.result}, nil
}

func (a *lifecycleTestAwaitService) Await(context.Context, event.System, event.Kind, event.Path, time.Duration) event.AwaitResult {
	return a.result
}
func (a *lifecycleTestAwaitService) Start(context.Context) error { return nil }
func (a *lifecycleTestAwaitService) Stop() error                 { return nil }

type lifecycleTestPool struct {
	method  string
	pid     pid.PID
	starts  int
	calls   int
	stopped bool
}

func (p *lifecycleTestPool) Call(ctx context.Context, method string, _ payload.Payloads) (*runtimeapi.Result, error) {
	p.calls++
	p.method = method
	p.pid, _ = runtimeapi.GetFramePID(ctx)
	return &runtimeapi.Result{Value: payload.New("ok")}, nil
}
func (p *lifecycleTestPool) Start() { p.starts++ }
func (p *lifecycleTestPool) Stop()  { p.stopped = true }
func (p *lifecycleTestPool) Send(*relay.Package) error {
	return nil
}

type lifecycleTestTopology struct {
	registerErr   error
	lastResult    *runtimeapi.Result
	lastPID       pid.PID
	completeCalls int
	registerCalls int
}

func (t *lifecycleTestTopology) Monitor(pid.PID, pid.PID) error { return nil }
func (t *lifecycleTestTopology) Demonitor(pid.PID, pid.PID) error {
	return nil
}
func (t *lifecycleTestTopology) Link(pid.PID, pid.PID) error { return nil }
func (t *lifecycleTestTopology) Unlink(pid.PID, pid.PID) error {
	return nil
}
func (t *lifecycleTestTopology) GetLinks(pid.PID) []pid.PID { return nil }
func (t *lifecycleTestTopology) Register(p pid.PID) error {
	t.registerCalls++
	t.lastPID = p
	return t.registerErr
}
func (t *lifecycleTestTopology) Complete(p pid.PID, result *runtimeapi.Result) {
	t.completeCalls++
	t.lastPID = p
	t.lastResult = result
}
func (t *lifecycleTestTopology) Remove(pid.PID) {}

type lifecycleTestPIDRegistry struct {
	removed []pid.PID
}

func (r *lifecycleTestPIDRegistry) Register(string, pid.PID) (pid.PID, error) { return pid.PID{}, nil }
func (r *lifecycleTestPIDRegistry) Unregister(string) bool                    { return false }
func (r *lifecycleTestPIDRegistry) Lookup(string) (pid.PID, bool)             { return pid.PID{}, false }
func (r *lifecycleTestPIDRegistry) Remove(p pid.PID) {
	r.removed = append(r.removed, p)
}

func TestLifecycleInvalidKind(t *testing.T) {
	m := NewManager(zap.NewNop(), &lifecycleTestBus{}, nil, nil)
	entry := registry.Entry{
		ID:   registry.NewID("app.test", "x"),
		Kind: "function.other",
	}

	if err := m.Add(context.Background(), entry); err == nil {
		t.Fatal("Add() expected invalid entry kind error")
	}
	if err := m.Update(context.Background(), entry); err == nil {
		t.Fatal("Update() expected invalid entry kind error")
	}
	if err := m.Delete(context.Background(), entry); err == nil {
		t.Fatal("Delete() expected invalid entry kind error")
	}
}

func TestDeleteRemovesPoolAndConfig(t *testing.T) {
	bus := &lifecycleTestBus{}
	m := NewManager(zap.NewNop(), bus, nil, nil)
	id := registry.NewID("app.test", "wasm")

	p := &lifecycleTestPool{}
	m.pools[id] = newPoolEntry(p, "run", "app.test:wasm#wasm.test")
	m.configs[id] = &configEntry{kind: wasmapi.FunctionWASM}

	err := m.Delete(context.Background(), registry.Entry{
		ID:   id,
		Kind: wasmapi.FunctionWASM,
	})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !p.stopped {
		t.Fatal("pool Stop() was not called")
	}
	if _, ok := m.pools[id]; ok {
		t.Fatal("pool entry still present after Delete()")
	}
	if _, ok := m.configs[id]; ok {
		t.Fatal("config entry still present after Delete()")
	}
	if len(bus.events) != 1 || bus.events[0].Kind != function.FunctionDelete {
		t.Fatalf("unexpected bus events: %#v", bus.events)
	}
}

func TestStartRegistersPreexistingPoolHost(t *testing.T) {
	m := NewManager(zap.NewNop(), &lifecycleTestBus{}, nil, nil)
	node := systemrelay.NewNode("test-node")
	id := registry.NewID("app.test", "preexisting_wasm")
	hostID := "app.test:preexisting_wasm#wasm.test"
	p := &lifecycleTestPool{}
	m.pools[id] = newPoolEntry(p, "run", hostID)

	ctx := relay.WithNode(ctxapi.NewRootContext(), node)
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if p.starts != 1 {
		t.Fatalf("pool starts = %d, want 1", p.starts)
	}

	registered, ok := node.GetHost(hostID)
	if !ok {
		t.Fatal("preexisting pool host was not registered on manager start")
	}
	if registered != p {
		t.Fatalf("registered host = %#v, want %#v", registered, p)
	}

	m.Stop()
	if _, ok := node.GetHost(hostID); ok {
		t.Fatal("manager stop did not unregister generation host")
	}
}

func TestExecuteUsesPoolGenerationHost(t *testing.T) {
	m := NewManager(zap.NewNop(), &lifecycleTestBus{}, nil, nil)
	id := registry.NewID("app.test", "wasm")
	hostID := "app.test:wasm#wasm.1"
	p := &lifecycleTestPool{}
	m.pools[id] = newPoolEntry(p, "run", hostID)

	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer func() { _ = fc.Close() }()
	if err := runtimeapi.SetFramePID(ctx, (&pid.PID{Host: id.String(), UniqID: "call-1"}).Precomputed()); err != nil {
		t.Fatalf("SetFramePID() error = %v", err)
	}

	result, err := m.Execute(ctx, runtimeapi.Task{ID: id})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result == nil {
		t.Fatal("Execute() returned nil result")
	}
	if p.calls != 1 || p.method != "run" {
		t.Fatalf("pool call = (%d, %q), want (1, run)", p.calls, p.method)
	}
	if p.pid.Host != hostID || p.pid.UniqID != "call-1" {
		t.Fatalf("pool pid = %#v, want host %q uniq call-1", p.pid, hostID)
	}

	framePID, ok := runtimeapi.GetFramePID(ctx)
	if !ok || framePID.Host != hostID || framePID.UniqID != "call-1" {
		t.Fatalf("frame pid = %#v, %v; want host %q uniq call-1", framePID, ok, hostID)
	}
}

func TestPoolEntryRetireWaitsForActiveExecution(t *testing.T) {
	entry := newPoolEntry(&lifecycleTestPool{}, "run", "app.test:wasm#wasm.1")
	if !entry.acquire() {
		t.Fatal("acquire() = false, want true")
	}

	stopped := make(chan struct{})
	entry.retire(func() {
		close(stopped)
	})

	select {
	case <-stopped:
		t.Fatal("retired pool stopped before active execution released")
	default:
	}
	if entry.acquire() {
		t.Fatal("retired entry accepted a new call")
	}
	entry.release()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("retired pool did not stop after active execution released")
	}
}

func TestRetiredPoolHostDrainsBeforeUnregister(t *testing.T) {
	m := NewManager(zap.NewNop(), &lifecycleTestBus{}, nil, nil)
	node := systemrelay.NewNode("test-node")
	m.node = node
	hostID := "app.test:wasm#wasm.retired"
	p := &lifecycleTestPool{}
	entry := newPoolEntry(p, "run", hostID)
	if err := node.RegisterHost(hostID, p); err != nil {
		t.Fatalf("RegisterHost() error = %v", err)
	}
	if !entry.acquire() {
		t.Fatal("acquire() = false, want true")
	}

	m.retirePoolEntry(entry)

	registered, ok := node.GetHost(hostID)
	if !ok {
		t.Fatal("active retired generation was unregistered too early")
	}
	if registered != p {
		t.Fatalf("registered host = %#v, want %#v", registered, p)
	}
	if p.stopped {
		t.Fatal("active retired generation stopped before release")
	}

	entry.release()

	require.Eventually(t, func() bool {
		_, exists := node.GetHost(hostID)
		return !exists && p.stopped
	}, time.Second, 10*time.Millisecond)
}

func TestLoadModuleInvalidKind(t *testing.T) {
	m := NewManager(zap.NewNop(), &lifecycleTestBus{}, nil, nil)
	_, err := m.loadModule(context.Background(), &configEntry{kind: "invalid.kind"})
	if err == nil {
		t.Fatal("loadModule() expected invalid kind error")
	}
}

func TestRegisterCaller(t *testing.T) {
	id := registry.NewID("app.test", "f")
	bus := &lifecycleTestBus{}
	m := NewManager(zap.NewNop(), bus, nil, nil)

	if err := m.registerCaller(ctxapi.NewRootContext(), id, nil); err == nil {
		t.Fatal("registerCaller() expected error without await service")
	}

	rejectCtx := event.WithAwaitService(ctxapi.NewRootContext(), &lifecycleTestAwaitService{
		result: event.AwaitResult{Accepted: false, Error: errors.New("reject")},
	})
	if err := m.registerCaller(rejectCtx, id, nil); err == nil {
		t.Fatal("registerCaller() expected reject error")
	}

	acceptCtx := event.WithAwaitService(ctxapi.NewRootContext(), &lifecycleTestAwaitService{
		result: event.AwaitResult{Accepted: true},
	})
	if err := m.registerCaller(acceptCtx, id, nil); err != nil {
		t.Fatalf("registerCaller() error = %v", err)
	}
	if len(bus.events) == 0 {
		t.Fatal("registerCaller() did not send event")
	}
	last := bus.events[len(bus.events)-1]
	if last.Kind != function.FunctionRegister || last.Path != id.String() {
		t.Fatalf("registerCaller() sent %#v", last)
	}
}

func TestRegisterCallerPreparesBeforeSend(t *testing.T) {
	id := registry.NewID("app.test", "f")
	bus := &lifecycleTestBus{}
	m := NewManager(zap.NewNop(), bus, nil, nil)
	awaitSvc := &lifecycleTestAwaitService{
		result: event.AwaitResult{Accepted: true},
	}

	sendBeforePrepare := false
	bus.onSend = func() {
		if !awaitSvc.prepared {
			sendBeforePrepare = true
		}
	}

	ctx := event.WithAwaitService(ctxapi.NewRootContext(), awaitSvc)
	if err := m.registerCaller(ctx, id, nil); err != nil {
		t.Fatalf("registerCaller() error = %v", err)
	}
	if sendBeforePrepare {
		t.Fatal("function register was sent before await prepare")
	}
}

func TestCreateExecutionHooks(t *testing.T) {
	m := NewManager(zap.NewNop(), &lifecycleTestBus{}, nil, nil)
	emptyHooks := m.createExecutionHooks()
	if emptyHooks.OnStart != nil || emptyHooks.OnComplete != nil {
		t.Fatal("createExecutionHooks() should return empty hooks without topology and pid registry")
	}

	topo := &lifecycleTestTopology{}
	pidReg := &lifecycleTestPIDRegistry{}
	m.topo = topo
	m.pidReg = pidReg

	hooks := m.createExecutionHooks()
	if hooks.OnStart == nil || hooks.OnComplete == nil {
		t.Fatal("createExecutionHooks() should provide hooks")
	}

	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer func() { _ = fc.Close() }()
	testPID := (&pid.PID{Host: "host", UniqID: "123"}).Precomputed()
	if err := runtimeapi.SetFramePID(ctx, testPID); err != nil {
		t.Fatalf("SetFramePID() error = %v", err)
	}

	hooks.OnStart(ctx, nil)
	if topo.registerCalls != 1 || topo.lastPID != testPID {
		t.Fatalf("OnStart() registerCalls = %d, lastPID = %v", topo.registerCalls, topo.lastPID)
	}

	result := &runtimeapi.Result{Error: supervisor.ErrExit}
	hooks.OnComplete(ctx, result)
	if result.Error != nil {
		t.Fatalf("OnComplete() should clear supervisor exit error, got %v", result.Error)
	}
	if topo.completeCalls != 1 {
		t.Fatalf("OnComplete() completeCalls = %d, want 1", topo.completeCalls)
	}
	if len(pidReg.removed) != 1 || pidReg.removed[0] != testPID {
		t.Fatalf("OnComplete() removed pids = %#v", pidReg.removed)
	}
}

func TestManagerConfigHelpers(t *testing.T) {
	m := NewManager(zap.NewNop(), &lifecycleTestBus{}, nil, nil)
	id := registry.NewID("app.test", "cfg")
	cfg := &configEntry{kind: wasmapi.FunctionWAT, method: "run"}

	m.storeConfig(id, cfg)
	got := m.getConfig(id)
	if got != cfg {
		t.Fatalf("getConfig() = %#v, want %#v", got, cfg)
	}
	m.deleteConfig(id)
	if m.getConfig(id) != nil {
		t.Fatal("getConfig() should return nil after deleteConfig()")
	}
}

func TestRuntimeInstanceSelectsCoreAndComponent(t *testing.T) {
	m := NewManager(zap.NewNop(), &lifecycleTestBus{}, nil, nil)
	core := new(wasmrt.Runtime)
	component := new(wasmrt.Runtime)
	m.coreRT = core
	m.componentRT = component

	if got := m.runtimeInstance(false); got != core {
		t.Fatalf("runtimeInstance(false) = %#v, want %#v", got, core)
	}
	if got := m.runtimeInstance(true); got != component {
		t.Fatalf("runtimeInstance(true) = %#v, want %#v", got, component)
	}
}

var _ event.Bus = (*lifecycleTestBus)(nil)
var _ event.AwaitService = (*lifecycleTestAwaitService)(nil)
var _ funcpool.Pool = (*lifecycleTestPool)(nil)
var _ topology.Topology = (*lifecycleTestTopology)(nil)
var _ topology.PIDRegistry = (*lifecycleTestPIDRegistry)(nil)
