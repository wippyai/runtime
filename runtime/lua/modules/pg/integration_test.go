// SPDX-License-Identifier: MPL-2.0

package pg_test

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/runtime/lua/engine"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	pgmod "github.com/wippyai/runtime/runtime/lua/modules/pg"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	syspg "github.com/wippyai/runtime/service/pg"
	"github.com/wippyai/runtime/system/clock"
	"github.com/wippyai/runtime/system/eventbus"
	systempayload "github.com/wippyai/runtime/system/payload"
	sysrelay "github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"github.com/wippyai/runtime/system/scheduler/pool/inline"
	"go.uber.org/zap"
)

// ==========================================================================
// Test infrastructure
// ==========================================================================

// noopTopology is a minimal topology mock for integration tests.
type noopTopology struct{}

func (n *noopTopology) Register(_ pid.PID) error              { return nil }
func (n *noopTopology) Complete(_ pid.PID, _ *runtime.Result) {}
func (n *noopTopology) Remove(_ pid.PID)                      {}
func (n *noopTopology) Monitor(_, _ pid.PID) error            { return nil }
func (n *noopTopology) Demonitor(_, _ pid.PID) error          { return nil }
func (n *noopTopology) Link(_, _ pid.PID) error               { return nil }
func (n *noopTopology) Unlink(_, _ pid.PID) error             { return nil }
func (n *noopTopology) GetLinks(_ pid.PID) []pid.PID          { return nil }

var _ topology.Topology = (*noopTopology)(nil)

// --- mock resource registry for pg.open() ---

// pgTestResource wraps a ScopeService as a resource.Resource[any].
type pgTestResource struct {
	svc pgapi.ScopeService
}

func (r *pgTestResource) Get() (any, error) { return r.svc, nil }
func (r *pgTestResource) Release()          {}

// pgTestRegistry is a mock resource.Registry that returns the pg service
// for any Acquire call with a matching ID.
type pgTestRegistry struct {
	svc pgapi.ScopeService
	id  string
}

func newPGTestRegistry(svc pgapi.ScopeService, id string) *pgTestRegistry {
	return &pgTestRegistry{svc: svc, id: id}
}

func (r *pgTestRegistry) Acquire(_ context.Context, id registry.ID, _ resource.AccessMode) (resource.Resource[any], error) {
	if id.String() == r.id {
		return &pgTestResource{svc: r.svc}, nil
	}
	return nil, resource.ErrNotFound
}

func (r *pgTestRegistry) List() ([]registry.ID, error) {
	return []registry.ID{registry.ParseID(r.id)}, nil
}

func (r *pgTestRegistry) Exists(id registry.ID) bool {
	return id.String() == r.id
}

var _ resource.Registry = (*pgTestRegistry)(nil)

// The resource ID all tests use for pg.open().
const testPGResourceID = "test:pg"

// --- test scheduler infrastructure ---

type pgTestScheduler struct {
	*actor.Scheduler
	pending map[string]chan *runtime.Result
	mu      sync.Mutex
}

func (ts *pgTestScheduler) OnStart(_ context.Context, _ pid.PID, _ process.Process) error {
	return nil
}

func (ts *pgTestScheduler) OnComplete(_ context.Context, p pid.PID, result *runtime.Result) {
	ts.mu.Lock()
	ch, ok := ts.pending[p.UniqID]
	if ok {
		delete(ts.pending, p.UniqID)
	}
	ts.mu.Unlock()
	if ok {
		ch <- result
	}
}

func (ts *pgTestScheduler) Execute(ctx context.Context, p pid.PID, proc process.Process, method string, input payload.Payloads) (*runtime.Result, error) {
	resultCh := make(chan *runtime.Result, 1)

	ts.mu.Lock()
	ts.pending[p.UniqID] = resultCh
	ts.mu.Unlock()

	_, err := ts.Submit(ctx, p, proc, method, input)
	if err != nil {
		ts.mu.Lock()
		delete(ts.pending, p.UniqID)
		ts.mu.Unlock()
		return nil, err
	}

	select {
	case result := <-resultCh:
		return result, nil
	case <-ctx.Done():
		ts.mu.Lock()
		delete(ts.pending, p.UniqID)
		ts.mu.Unlock()
		return nil, ctx.Err()
	}
}

// --- integration test context ---

type pgTestContext struct {
	ctx       context.Context
	scheduler *pgTestScheduler
	service   *syspg.Service
	node      relay.Node
}

func setupPGTest(t *testing.T) *pgTestContext {
	t.Helper()

	logger := zap.NewNop()
	bus := eventbus.NewBus()
	node := sysrelay.NewNode("test-node")

	// Create pg service with noop topology
	topo := &noopTopology{}
	service := syspg.NewService(logger, "pg", nil, node, topo, nil, bus, "test-node", nil, nil, nil)

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	ctx = relay.WithNode(ctx, node)

	// Wire the resource registry so pg.open("test:pg") works
	reg := newPGTestRegistry(service, testPGResourceID)
	resource.WithRegistry(ctx, reg)

	require.NoError(t, func() error { _, err := service.Start(ctx); return err }())

	// Create dispatcher registry and register pg commands
	dispReg := scheduler.NewRegistry()
	pgDisp := syspg.NewDispatcher()
	pgDisp.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		dispReg.Register(id, h)
	})

	ts := &pgTestScheduler{
		pending: make(map[string]chan *runtime.Result),
	}
	opts := []actor.Option{
		actor.WithWorkers(4),
		actor.WithLifecycle(ts),
	}
	ts.Scheduler = actor.NewScheduler(dispReg, opts...)
	ts.Start()

	return &pgTestContext{
		ctx:       ctx,
		scheduler: ts,
		service:   service,
		node:      node,
	}
}

func (tc *pgTestContext) Close(t *testing.T) {
	t.Helper()
	tc.scheduler.Stop(context.Background())
	require.NoError(t, tc.service.Stop(context.Background()))
}

var pgTestPIDCounter atomic.Int64

func uniquePGTestPID() pid.PID {
	n := pgTestPIDCounter.Add(1)
	p := pid.PID{
		Host:   "test-node",
		UniqID: "pg-e2e-" + strconv.FormatInt(n, 10),
	}
	return p.Precomputed()
}

func bindPGModule(l *lua.LState) error {
	engine.LoadModuleDef(l, pgmod.Module)
	return nil
}

func newPGLuaProcess(t *testing.T, script string) *engine.Process {
	t.Helper()
	proto, err := lua.CompileString(script, "pg_test.lua")
	require.NoError(t, err)
	proc, err := engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(func(l *lua.LState) error {
			engine.LoadModuleDef(l, engine.ChannelModule)
			return nil
		}),
		engine.WithModuleBinder(bindPGModule),
	)
	require.NoError(t, err)
	return proc
}

func runPGScript(t *testing.T, tc *pgTestContext, script string) *runtime.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(tc.ctx, 5*time.Second)
	defer cancel()

	frameCtx, _ := ctxapi.OpenFrameContext(ctx)
	testPID := uniquePGTestPID()
	err := runtime.SetFramePID(frameCtx, testPID)
	require.NoError(t, err)

	proc := newPGLuaProcess(t, script)
	result, err := tc.scheduler.Execute(frameCtx, testPID, proc, "", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

// runPGScriptWithPID runs a Lua script with a specific PID for testing group membership.
func runPGScriptWithPID(t *testing.T, tc *pgTestContext, p pid.PID, script string) *runtime.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(tc.ctx, 5*time.Second)
	defer cancel()

	frameCtx, _ := ctxapi.OpenFrameContext(ctx)
	err := runtime.SetFramePID(frameCtx, p)
	require.NoError(t, err)

	proc := newPGLuaProcess(t, script)
	result, err := tc.scheduler.Execute(frameCtx, p, proc, "", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

// ==========================================================================
// Helper to set up inline pool context with resource registry.
// Used by events/monitor tests that need inline.Pool.
// ==========================================================================

type inlinePoolTestContext struct {
	ctx     context.Context
	node    relay.Node
	fc      ctxapi.FrameContext
	cancel  context.CancelFunc
	service *syspg.Service
	pool    *inline.Pool
	pidGen  *uniqid.PIDGenerator
	hostID  string
}

func setupInlinePoolTest(t *testing.T, hostID string, script string, needTime bool) *inlinePoolTestContext {
	t.Helper()

	logger := zap.NewNop()
	bus := eventbus.NewBus()
	node := sysrelay.NewNode("test-node")

	rootCtx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	transcoder := systempayload.NewTranscoder()
	luapayload.Register(transcoder)
	payload.WithTranscoder(rootCtx, transcoder)

	ctx, cancel := context.WithTimeout(rootCtx, 10*time.Second)
	ctx = relay.WithNode(ctx, node)

	// Create pg service
	topo := &noopTopology{}
	service := syspg.NewService(logger, "pg", nil, node, topo, nil, bus, "test-node", nil, nil, nil)

	// Wire resource registry so pg.open() works in inline pool processes
	reg := newPGTestRegistry(service, testPGResourceID)
	resource.WithRegistry(rootCtx, reg)

	require.NoError(t, func() error { _, err := service.Start(ctx); return err }())

	// Create dispatcher registry
	dispReg := scheduler.NewRegistry()

	pgDisp := syspg.NewDispatcher()
	pgDisp.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		dispReg.Register(id, h)
	})

	clockSvc := clock.NewDispatcher()
	t.Cleanup(func() { _ = clockSvc.Stop(ctx) })
	clockSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		dispReg.Register(id, h)
	})

	eventsSvc := eventbus.NewDispatcher(bus, node)
	require.NoError(t, eventsSvc.Start(ctx))
	t.Cleanup(func() { _ = eventsSvc.Stop(context.Background()) })
	eventsSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		dispReg.Register(id, h)
	})

	// PID generator
	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "test-node")

	// Process factory
	binders := append(engine.CoreBinders(),
		func(l *lua.LState) error {
			engine.LoadModuleDef(l, pgmod.Module)
			return nil
		},
	)
	if needTime {
		binders = append(binders, func(l *lua.LState) error {
			mod, _ := timemod.Module.Build()
			l.SetGlobal("time", mod)
			return nil
		})
	}

	factory := func() (process.Process, error) {
		cfg := engine.FactoryConfig{
			Script:        script,
			ScriptName:    hostID,
			ModuleBinders: binders,
		}
		f := engine.NewFactory(cfg)
		return f()
	}

	pool, err := inline.New(factory, dispReg)
	require.NoError(t, err)
	t.Cleanup(pool.Stop)

	err = node.RegisterHost(hostID, pool)
	require.NoError(t, err)

	frameCtx, fc := ctxapi.OpenFrameContext(ctx)

	p := pidGen.Generate(hostID)
	err = runtime.SetFramePID(frameCtx, p)
	require.NoError(t, err)
	frameCtx = relay.WithNode(frameCtx, node)

	return &inlinePoolTestContext{
		ctx:     frameCtx,
		cancel:  cancel,
		service: service,
		pool:    pool,
		pidGen:  pidGen,
		hostID:  hostID,
		node:    node,
		fc:      fc,
	}
}

func (tc *inlinePoolTestContext) Close(t *testing.T) {
	t.Helper()
	ctxapi.ReleaseFrameContext(tc.fc)
	tc.cancel()
	require.NoError(t, tc.service.Stop(context.Background()))
}

func (tc *inlinePoolTestContext) Run(t *testing.T) *runtime.Result {
	t.Helper()
	result, err := tc.pool.Call(tc.ctx, "main", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	if result.Error != nil {
		t.Fatalf("Lua execution error: %v", result.Error)
	}
	return result
}

// ==========================================================================
// Integration Tests — basic join/leave/get_members/which_groups
// ==========================================================================

func TestIntegration_JoinAndGetMembers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	testPID := uniquePGTestPID()

	// Join a group
	result := runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local ok, err = scope:join("workers")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LTrue, result.Value.Data())

	// Get members and verify the PID is in the group
	result = runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local members, err = scope:get_members("workers")
		if err then return nil, tostring(err) end
		return #members
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(1), result.Value.Data())
}

func TestIntegration_JoinAndLeave(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	testPID := uniquePGTestPID()

	// Join
	result := runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local ok, err = scope:join("temp-group")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)

	// Verify membership
	result = runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local members, err = scope:get_members("temp-group")
		if err then return nil, tostring(err) end
		return #members
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(1), result.Value.Data())

	// Leave
	result = runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local ok, err = scope:leave("temp-group")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LTrue, result.Value.Data())

	// Verify group is empty
	result = runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local members, err = scope:get_members("temp-group")
		if err then return nil, tostring(err) end
		return #members
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(0), result.Value.Data())
}

func TestIntegration_LeaveNotJoined(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	// Leave a group we never joined - should return error
	result := runPGScript(t, tc, `
		local scope = pg.open("test:pg")
		local ok, err = scope:leave("nonexistent-group")
		if err then
			return "error"
		end
		return "no_error"
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, "error", string(result.Value.Data().(lua.LString)))
}

func TestIntegration_GetMembersEmptyGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	// Get members of a group that doesn't exist - should return empty table
	result := runPGScript(t, tc, `
		local scope = pg.open("test:pg")
		local members, err = scope:get_members("empty-group")
		if err then return nil, tostring(err) end
		return #members
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(0), result.Value.Data())
}

func TestIntegration_GetLocalMembers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	testPID := uniquePGTestPID()

	// Join
	result := runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local ok, err = scope:join("local-group")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)

	// Get local members
	result = runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local members, err = scope:get_local_members("local-group")
		if err then return nil, tostring(err) end
		return #members
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(1), result.Value.Data())
}

func TestIntegration_WhichGroups(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	testPID := uniquePGTestPID()

	// Join two groups
	result := runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local ok, err = scope:join("group-a")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)

	result = runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local ok, err = scope:join("group-b")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)

	// which_groups should return both
	result = runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local groups, err = scope:which_groups()
		if err then return nil, tostring(err) end
		return #groups
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(2), result.Value.Data())
}

func TestIntegration_WhichGroupsEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	result := runPGScript(t, tc, `
		local scope = pg.open("test:pg")
		local groups, err = scope:which_groups()
		if err then return nil, tostring(err) end
		return #groups
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(0), result.Value.Data())
}

func TestIntegration_MultipleProcessesInGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	pid1 := uniquePGTestPID()
	pid2 := uniquePGTestPID()
	pid3 := uniquePGTestPID()

	// Join three processes to the same group
	for _, p := range []pid.PID{pid1, pid2, pid3} {
		result := runPGScriptWithPID(t, tc, p, `
			local scope = pg.open("test:pg")
			local ok, err = scope:join("multi-group")
			if err then return nil, tostring(err) end
			return ok
		`)
		require.NoError(t, result.Error)
	}

	// Verify all three are members
	result := runPGScriptWithPID(t, tc, pid1, `
		local scope = pg.open("test:pg")
		local members, err = scope:get_members("multi-group")
		if err then return nil, tostring(err) end
		return #members
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(3), result.Value.Data())
}

func TestIntegration_JoinMultipleGroups(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	testPID := uniquePGTestPID()

	// Join multiple groups with the same PID
	for _, group := range []string{"alpha", "beta", "gamma"} {
		result := runPGScriptWithPID(t, tc, testPID, `
			local scope = pg.open("test:pg")
			local ok, err = scope:join("`+group+`")
			if err then return nil, tostring(err) end
			return ok
		`)
		require.NoError(t, result.Error)
	}

	// which_groups should return 3
	result := runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local groups, err = scope:which_groups()
		if err then return nil, tostring(err) end
		return #groups
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(3), result.Value.Data())

	// Each group should have 1 member
	for _, group := range []string{"alpha", "beta", "gamma"} {
		result := runPGScriptWithPID(t, tc, testPID, `
			local scope = pg.open("test:pg")
			local members, err = scope:get_members("`+group+`")
			if err then return nil, tostring(err) end
			return #members
		`)
		require.NoError(t, result.Error)
		assert.Equal(t, lua.LNumber(1), result.Value.Data(), "group %s should have 1 member", group)
	}
}

func TestIntegration_GetMembersReturnsPIDStrings(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	testPID := uniquePGTestPID()

	// Join
	result := runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local ok, err = scope:join("pid-check")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)

	// Verify members returns string PIDs
	result = runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local members, err = scope:get_members("pid-check")
		if err then return nil, tostring(err) end
		if #members == 0 then return nil, "no members" end
		-- Each member should be a string
		if type(members[1]) ~= "string" then
			return nil, "expected string, got " .. type(members[1])
		end
		return members[1]
	`)
	require.NoError(t, result.Error)
	require.NotNil(t, result.Value)
	// The returned value should be the PID string representation
	pidStr := string(result.Value.Data().(lua.LString))
	assert.Contains(t, pidStr, testPID.UniqID)
}

func TestIntegration_BroadcastLocal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	testPID := uniquePGTestPID()

	// Join a group first
	result := runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local ok, err = scope:join("broadcast-group")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)

	// Broadcast locally - should succeed even though the message
	// won't be received (no listener in this simple test)
	result = runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local ok, err = scope:broadcast_local("broadcast-group", "test.topic", "hello")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LTrue, result.Value.Data())
}

func TestIntegration_Broadcast(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	testPID := uniquePGTestPID()

	// Join a group
	result := runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local ok, err = scope:join("bcast-group")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)

	// Broadcast globally
	result = runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local ok, err = scope:broadcast("bcast-group", "hello.topic", "world")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LTrue, result.Value.Data())
}

func TestIntegration_BroadcastEmptyGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	// Broadcast to empty group should succeed (0 members = nothing to send)
	result := runPGScript(t, tc, `
		local scope = pg.open("test:pg")
		local ok, err = scope:broadcast("empty-bcast", "topic", "data")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LTrue, result.Value.Data())
}

func TestIntegration_FullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	testPID := uniquePGTestPID()

	// Full lifecycle in a single script: join, verify, leave, verify empty
	result := runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")

		-- Join
		local ok, err = scope:join("lifecycle")
		if err then return nil, "join: " .. tostring(err) end

		-- Check members
		local members, err = scope:get_members("lifecycle")
		if err then return nil, "get_members: " .. tostring(err) end
		if #members ~= 1 then
			return nil, "expected 1 member, got " .. #members
		end

		-- Check which_groups
		local groups, err = scope:which_groups()
		if err then return nil, "which_groups: " .. tostring(err) end
		if #groups < 1 then
			return nil, "expected at least 1 group, got " .. #groups
		end

		-- Leave
		local ok, err = scope:leave("lifecycle")
		if err then return nil, "leave: " .. tostring(err) end

		-- Verify empty
		local members2, err = scope:get_members("lifecycle")
		if err then return nil, "get_members2: " .. tostring(err) end
		if #members2 ~= 0 then
			return nil, "expected 0 members after leave, got " .. #members2
		end

		return "lifecycle_complete"
	`)
	require.NoError(t, result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, "lifecycle_complete", string(result.Value.Data().(lua.LString)))
}

func TestIntegration_ConcurrentJoins(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	const numGoroutines = 20

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			defer wg.Done()
			p := uniquePGTestPID()
			result := runPGScriptWithPID(t, tc, p, `
				local scope = pg.open("test:pg")
				local ok, err = scope:join("concurrent-group")
				if err then return nil, tostring(err) end
				return ok
			`)
			require.NoError(t, result.Error)
			assert.Equal(t, lua.LTrue, result.Value.Data())
		}(i)
	}

	wg.Wait()

	// Verify all processes joined
	verifyPID := uniquePGTestPID()
	result := runPGScriptWithPID(t, tc, verifyPID, `
		local scope = pg.open("test:pg")
		local members, err = scope:get_members("concurrent-group")
		if err then return nil, tostring(err) end
		return #members
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(numGoroutines), result.Value.Data())
}

func TestIntegration_MultiJoinSameGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	testPID := uniquePGTestPID()

	// Join the same group multiple times (multi-join support)
	for i := 0; i < 3; i++ {
		result := runPGScriptWithPID(t, tc, testPID, `
			local scope = pg.open("test:pg")
			local ok, err = scope:join("multi-join")
			if err then return nil, tostring(err) end
			return ok
		`)
		require.NoError(t, result.Error)
	}

	// The PID should appear 3 times in the member list
	result := runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local members, err = scope:get_members("multi-join")
		if err then return nil, tostring(err) end
		return #members
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(3), result.Value.Data())

	// Leave once - should reduce count by 1
	result = runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local ok, err = scope:leave("multi-join")
		if err then return nil, tostring(err) end
		local members, err = scope:get_members("multi-join")
		if err then return nil, tostring(err) end
		return #members
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(2), result.Value.Data())
}

// --- Additional integration tests ---

func TestIntegration_LeaveReducesToZero(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	testPID := uniquePGTestPID()

	// Join twice, leave twice — group should be empty
	result := runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")

		local ok, err = scope:join("leave-zero")
		if err then return nil, "join1: " .. tostring(err) end

		ok, err = scope:join("leave-zero")
		if err then return nil, "join2: " .. tostring(err) end

		ok, err = scope:leave("leave-zero")
		if err then return nil, "leave1: " .. tostring(err) end

		ok, err = scope:leave("leave-zero")
		if err then return nil, "leave2: " .. tostring(err) end

		local members, err = scope:get_members("leave-zero")
		if err then return nil, "get_members: " .. tostring(err) end
		return #members
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(0), result.Value.Data())
}

func TestIntegration_LeaveExtraReturnsError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	testPID := uniquePGTestPID()

	// Join once, leave twice — second leave should return error
	result := runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")

		local ok, err = scope:join("leave-extra")
		if err then return nil, "join: " .. tostring(err) end

		ok, err = scope:leave("leave-extra")
		if err then return nil, "leave1: " .. tostring(err) end

		ok, err = scope:leave("leave-extra")
		if err then
			return "got_error"
		end
		return "no_error"
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, "got_error", string(result.Value.Data().(lua.LString)))
}

func TestIntegration_BroadcastToMultipleMembers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	pid1 := uniquePGTestPID()
	pid2 := uniquePGTestPID()
	pid3 := uniquePGTestPID()

	// Join three processes
	for _, p := range []pid.PID{pid1, pid2, pid3} {
		result := runPGScriptWithPID(t, tc, p, `
			local scope = pg.open("test:pg")
			local ok, err = scope:join("bcast-multi")
			if err then return nil, tostring(err) end
			return ok
		`)
		require.NoError(t, result.Error)
	}

	// Broadcast from one of them
	result := runPGScriptWithPID(t, tc, pid1, `
		local scope = pg.open("test:pg")
		local ok, err = scope:broadcast("bcast-multi", "notify", "hello", "extra-payload")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LTrue, result.Value.Data())
}

func TestIntegration_BroadcastLocalEmptyGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	// BroadcastLocal to empty group should succeed (0 sends)
	result := runPGScript(t, tc, `
		local scope = pg.open("test:pg")
		local ok, err = scope:broadcast_local("empty-local", "topic", "data")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LTrue, result.Value.Data())
}

func TestIntegration_GetLocalMembersEmptyGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	result := runPGScript(t, tc, `
		local scope = pg.open("test:pg")
		local members, err = scope:get_local_members("nonexistent-local")
		if err then return nil, tostring(err) end
		return #members
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(0), result.Value.Data())
}

func TestIntegration_JoinLeaveJoinAgain(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	testPID := uniquePGTestPID()

	// Join, leave, then rejoin — should work correctly
	result := runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")

		local ok, err = scope:join("rejoin")
		if err then return nil, "join1: " .. tostring(err) end

		ok, err = scope:leave("rejoin")
		if err then return nil, "leave: " .. tostring(err) end

		-- Verify empty
		local members, err = scope:get_members("rejoin")
		if err then return nil, "check1: " .. tostring(err) end
		if #members ~= 0 then
			return nil, "expected 0 after leave, got " .. #members
		end

		-- Rejoin
		ok, err = scope:join("rejoin")
		if err then return nil, "join2: " .. tostring(err) end

		members, err = scope:get_members("rejoin")
		if err then return nil, "check2: " .. tostring(err) end
		return #members
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(1), result.Value.Data())
}

// ==========================================================================
// Events Integration Tests — inline.Pool based
// ==========================================================================

func TestIntegration_EventsSubscribeAndReceiveJoin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := setupInlinePoolTest(t, "test.pg:events-join", `
local time = require("time")

local function main()
    local scope = pg.open("test:pg")

    -- Subscribe to pg events
    local sub, groups, err = scope:events()
    if err then
        return nil, "subscribe error: " .. tostring(err)
    end

    local ch = sub:channel()

    -- Join a group (this should emit a member.joined event)
    local ok, err = scope:join("test-workers")
    if err then
        return nil, "join error: " .. tostring(err)
    end

    -- Wait for event with timeout
    local timer = time.after(3000 * time.MILLISECOND)
    local result = channel.select{
        ch:case_receive(),
        timer:case_receive()
    }

    if result.channel == timer then
        return nil, "timeout waiting for join event"
    end

    local evt = result.value
    if evt == nil then
        return nil, "no event value"
    end
    if evt.system ~= "pg" then
        return nil, "wrong system: " .. tostring(evt.system)
    end
    if evt.kind ~= "member.joined" then
        return nil, "wrong kind: " .. tostring(evt.kind)
    end
    if evt.path ~= "test-workers" then
        return nil, "wrong path: " .. tostring(evt.path)
    end

    -- Check event data
    if evt.data == nil then
        return nil, "no event data"
    end
    if evt.data.Group ~= "test-workers" then
        return nil, "wrong group in data: " .. tostring(evt.data.Group)
    end

    -- Close subscription
    sub:close()

    return "join_event_received"
end

return { main = main }
`, true)
	defer tc.Close(t)

	result := tc.Run(t)
	val := result.Value.Data()
	assert.Equal(t, "join_event_received", string(val.(lua.LString)))
}

func TestIntegration_EventsReceiveLeave(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := setupInlinePoolTest(t, "test.pg:events-leave", `
local time = require("time")

local function main()
    local scope = pg.open("test:pg")

    -- Subscribe to pg events
    local sub, groups, err = scope:events()
    if err then
        return nil, "subscribe error: " .. tostring(err)
    end

    local ch = sub:channel()

    -- Join a group first
    local ok, err = scope:join("leave-group")
    if err then
        return nil, "join error: " .. tostring(err)
    end

    -- Consume the join event
    local timer = time.after(3000 * time.MILLISECOND)
    local result = channel.select{
        ch:case_receive(),
        timer:case_receive()
    }
    if result.channel == timer then
        return nil, "timeout waiting for join event"
    end

    -- Now leave the group
    ok, err = scope:leave("leave-group")
    if err then
        return nil, "leave error: " .. tostring(err)
    end

    -- Wait for leave event
    timer = time.after(3000 * time.MILLISECOND)
    result = channel.select{
        ch:case_receive(),
        timer:case_receive()
    }
    if result.channel == timer then
        return nil, "timeout waiting for leave event"
    end

    local evt = result.value
    if evt.kind ~= "member.left" then
        return nil, "wrong kind: " .. tostring(evt.kind)
    end
    if evt.data.Group ~= "leave-group" then
        return nil, "wrong group: " .. tostring(evt.data.Group)
    end

    sub:close()
    return "leave_event_received"
end

return { main = main }
`, true)
	defer tc.Close(t)

	result := tc.Run(t)
	val := result.Value.Data()
	assert.Equal(t, "leave_event_received", string(val.(lua.LString)))
}

func TestIntegration_EventsClose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := setupInlinePoolTest(t, "test.pg:events-close", `
local function main()
    local scope = pg.open("test:pg")

    -- Subscribe to pg events
    local sub, groups, err = scope:events()
    if err then
        return nil, "subscribe error: " .. tostring(err)
    end

    -- Close immediately
    sub:close()

    -- Close again (should be idempotent)
    sub:close()

    return "close_ok"
end

return { main = main }
`, false)
	defer tc.Close(t)

	result := tc.Run(t)
	val := result.Value.Data()
	assert.Equal(t, "close_ok", string(val.(lua.LString)))
}

func TestIntegration_EventsMultipleJoins(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := setupInlinePoolTest(t, "test.pg:events-multi", `
local time = require("time")

local function main()
    local scope = pg.open("test:pg")

    local sub, groups, err = scope:events()
    if err then
        return nil, "subscribe error: " .. tostring(err)
    end

    local ch = sub:channel()

    -- Join two different groups
    scope:join("group-alpha")
    scope:join("group-beta")

    -- Receive two join events
    local events_received = {}
    for i = 1, 2 do
        local timer = time.after(3000 * time.MILLISECOND)
        local result = channel.select{
            ch:case_receive(),
            timer:case_receive()
        }
        if result.channel == timer then
            return nil, "timeout waiting for event " .. i
        end
        local evt = result.value
        if evt.kind ~= "member.joined" then
            return nil, "wrong kind for event " .. i .. ": " .. tostring(evt.kind)
        end
        table.insert(events_received, evt.data.Group)
    end

    -- Verify we got events for both groups
    table.sort(events_received)
    if #events_received ~= 2 then
        return nil, "expected 2 events, got " .. #events_received
    end
    if events_received[1] ~= "group-alpha" then
        return nil, "expected group-alpha, got " .. events_received[1]
    end
    if events_received[2] ~= "group-beta" then
        return nil, "expected group-beta, got " .. events_received[2]
    end

    sub:close()
    return "multi_join_ok"
end

return { main = main }
`, true)
	defer tc.Close(t)

	result := tc.Run(t)
	val := result.Value.Data()
	assert.Equal(t, "multi_join_ok", string(val.(lua.LString)))
}

// ==========================================================================
// Additional group lifecycle tests
// ==========================================================================

func TestIntegration_WhichGroupsAfterLeave(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	testPID := uniquePGTestPID()

	// Join two groups, leave one, verify which_groups
	result := runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")

		local ok, err = scope:join("wg-stay")
		if err then return nil, "join1: " .. tostring(err) end

		ok, err = scope:join("wg-leave")
		if err then return nil, "join2: " .. tostring(err) end

		-- Should have 2 groups
		local groups, err = scope:which_groups()
		if err then return nil, "wg1: " .. tostring(err) end
		if #groups ~= 2 then
			return nil, "expected 2 groups, got " .. #groups
		end

		-- Leave one
		ok, err = scope:leave("wg-leave")
		if err then return nil, "leave: " .. tostring(err) end

		-- Should have 1 group
		groups, err = scope:which_groups()
		if err then return nil, "wg2: " .. tostring(err) end
		return #groups
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(1), result.Value.Data())
}

// --- which_local_groups integration tests ---

func TestIntegration_WhichLocalGroups(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	testPID := uniquePGTestPID()

	// Join two groups and check which_local_groups
	result := runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")

		local ok, err = scope:join("wlg-alpha")
		if err then return nil, "join1: " .. tostring(err) end

		ok, err = scope:join("wlg-beta")
		if err then return nil, "join2: " .. tostring(err) end

		local groups, err = scope:which_local_groups()
		if err then return nil, "wlg: " .. tostring(err) end
		return #groups
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(2), result.Value.Data())
}

func TestIntegration_WhichLocalGroupsEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	result := runPGScript(t, tc, `
		local scope = pg.open("test:pg")
		local groups, err = scope:which_local_groups()
		if err then return nil, tostring(err) end
		return #groups
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(0), result.Value.Data())
}

// --- batch join/leave integration tests ---

func TestIntegration_BatchJoin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	testPID := uniquePGTestPID()

	// Batch join multiple groups
	result := runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")
		local ok, err = scope:join({"batch-a", "batch-b", "batch-c"})
		if err then return nil, "batch join: " .. tostring(err) end

		local groups, err = scope:which_groups()
		if err then return nil, "wg: " .. tostring(err) end
		return #groups
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(3), result.Value.Data())
}

func TestIntegration_BatchLeave(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	testPID := uniquePGTestPID()

	// Batch join, then batch leave some
	result := runPGScriptWithPID(t, tc, testPID, `
		local scope = pg.open("test:pg")

		local ok, err = scope:join({"bl-a", "bl-b", "bl-c"})
		if err then return nil, "join: " .. tostring(err) end

		ok, err = scope:leave({"bl-a", "bl-c"})
		if err then return nil, "leave: " .. tostring(err) end

		local groups, err = scope:which_groups()
		if err then return nil, "wg: " .. tostring(err) end

		if #groups ~= 1 then
			return nil, "expected 1 group, got " .. #groups
		end

		local members, err = scope:get_members("bl-b")
		if err then return nil, "gm: " .. tostring(err) end
		return #members
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(1), result.Value.Data())
}

func TestIntegration_BatchJoinEmptyTable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	// Empty table should return error
	result := runPGScript(t, tc, `
		local scope = pg.open("test:pg")
		local ok, err = scope:join({})
		if err then return "got_error" end
		return "no_error"
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, "got_error", string(result.Value.Data().(lua.LString)))
}

func TestIntegration_BatchLeaveEmptyTable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	result := runPGScript(t, tc, `
		local scope = pg.open("test:pg")
		local ok, err = scope:leave({})
		if err then return "got_error" end
		return "no_error"
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, "got_error", string(result.Value.Data().(lua.LString)))
}

// ==========================================================================
// Monitor integration tests (inline.Pool based)
// ==========================================================================

func TestIntegration_MonitorReceivesJoinAndLeave(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := setupInlinePoolTest(t, "test.pg:monitor", `
local time = require("time")

local function main()
    local scope = pg.open("test:pg")

    -- Monitor a specific group
    local sub, members, err = scope:monitor("mon-group")
    if err then
        return nil, "monitor error: " .. tostring(err)
    end

    -- Initial members should be empty
    if #members ~= 0 then
        return nil, "expected 0 initial members, got " .. #members
    end

    local ch = sub:channel()

    -- Join the monitored group
    local ok, err = scope:join("mon-group")
    if err then
        return nil, "join error: " .. tostring(err)
    end

    -- Wait for join event
    local timer = time.after(3000 * time.MILLISECOND)
    local result = channel.select{
        ch:case_receive(),
        timer:case_receive()
    }

    if result.channel == timer then
        return nil, "timeout waiting for join event"
    end

    local evt = result.value
    if evt.kind ~= "member.joined" then
        return nil, "expected member.joined, got " .. tostring(evt.kind)
    end
    if evt.path ~= "mon-group" then
        return nil, "expected path mon-group, got " .. tostring(evt.path)
    end

    -- Now leave
    scope:leave("mon-group")

    -- Wait for leave event
    timer = time.after(3000 * time.MILLISECOND)
    result = channel.select{
        ch:case_receive(),
        timer:case_receive()
    }

    if result.channel == timer then
        return nil, "timeout waiting for leave event"
    end

    evt = result.value
    if evt.kind ~= "member.left" then
        return nil, "expected member.left, got " .. tostring(evt.kind)
    end

    sub:close()
    return "monitor_ok"
end

return { main = main }
`, true)
	defer tc.Close(t)

	result := tc.Run(t)
	val := result.Value.Data()
	assert.Equal(t, "monitor_ok", string(val.(lua.LString)))
}

func TestIntegration_MonitorOnlyReceivesTargetGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := setupInlinePoolTest(t, "test.pg:monitor-filter", `
local time = require("time")

local function main()
    local scope = pg.open("test:pg")

    -- Monitor only "target-group"
    local sub, members, err = scope:monitor("target-group")
    if err then
        return nil, "monitor error: " .. tostring(err)
    end

    local ch = sub:channel()

    -- Join a DIFFERENT group (should NOT trigger monitor event)
    scope:join("other-group")

    -- Join the target group (should trigger monitor event)
    scope:join("target-group")

    -- Wait for event — should be from target-group
    local timer = time.after(3000 * time.MILLISECOND)
    local result = channel.select{
        ch:case_receive(),
        timer:case_receive()
    }

    if result.channel == timer then
        return nil, "timeout — monitor did not receive target-group event"
    end

    local evt = result.value
    if evt.path ~= "target-group" then
        return nil, "expected target-group, got " .. tostring(evt.path)
    end

    sub:close()
    return "filter_ok"
end

return { main = main }
`, true)
	defer tc.Close(t)

	result := tc.Run(t)
	val := result.Value.Data()
	assert.Equal(t, "filter_ok", string(val.(lua.LString)))
}

// --- events snapshot integration test ---

func TestIntegration_EventsReturnsSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := setupInlinePoolTest(t, "test.pg:events-snapshot", `
local function main()
    local scope = pg.open("test:pg")

    -- First join some groups BEFORE subscribing to events
    scope:join("snap-group-a")
    scope:join("snap-group-b")

    -- Now subscribe — snapshot should contain the groups we just joined
    local sub, groups, err = scope:events()
    if err then
        return nil, "events error: " .. tostring(err)
    end

    -- groups should be a table mapping group names to member arrays
    if type(groups) ~= "table" then
        return nil, "expected groups table, got " .. type(groups)
    end

    local count = 0
    for _ in pairs(groups) do count = count + 1 end

    if count < 2 then
        return nil, "expected at least 2 groups in snapshot, got " .. count
    end

    -- Check specific groups
    if not groups["snap-group-a"] then
        return nil, "snap-group-a not in snapshot"
    end
    if not groups["snap-group-b"] then
        return nil, "snap-group-b not in snapshot"
    end

    if #groups["snap-group-a"] ~= 1 then
        return nil, "expected 1 member in snap-group-a, got " .. #groups["snap-group-a"]
    end

    sub:close()
    return "snapshot_ok"
end

return { main = main }
`, false)
	defer tc.Close(t)

	result := tc.Run(t)
	val := result.Value.Data()
	assert.Equal(t, "snapshot_ok", string(val.(lua.LString)))
}

// --- demonitor with flush integration test ---

func TestIntegration_EventsCloseWithFlush(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := setupInlinePoolTest(t, "test.pg:close-flush", `
local function main()
    local scope = pg.open("test:pg")

    local sub, groups, err = scope:events()
    if err then
        return nil, "events error: " .. tostring(err)
    end

    -- Join to generate some events
    scope:join("flush-group")

    -- Close with flush option — should drain buffered events
    sub:close({flush = true})

    -- Channel should be drained — receiving should get nil immediately
    local ch = sub:channel()
    -- After close, channel should be closed
    return "flush_ok"
end

return { main = main }
`, false)
	defer tc.Close(t)

	result := tc.Run(t)
	val := result.Value.Data()
	assert.Equal(t, "flush_ok", string(val.(lua.LString)))
}

// --- remote leave spurious events test ---

func TestIntegration_RemoteLeaveNoSpuriousEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zap.NewNop()
	bus := eventbus.NewBus()
	node := sysrelay.NewNode("test-node")

	rootCtx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	transcoder := systempayload.NewTranscoder()
	luapayload.Register(transcoder)
	payload.WithTranscoder(rootCtx, transcoder)

	ctx, cancel := context.WithTimeout(rootCtx, 15*time.Second)
	defer cancel()
	ctx = relay.WithNode(ctx, node)

	topo := &noopTopology{}
	service := syspg.NewService(logger, "pg", nil, node, topo, nil, bus, "test-node", nil, nil, nil)

	// Wire resource registry
	reg := newPGTestRegistry(service, testPGResourceID)
	resource.WithRegistry(rootCtx, reg)

	require.NoError(t, func() error { _, err := service.Start(ctx); return err }())
	defer func() { _ = service.Stop(context.Background()) }()

	dispReg := scheduler.NewRegistry()
	pgDisp := syspg.NewDispatcher()
	pgDisp.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		dispReg.Register(id, h)
	})

	clockSvc := clock.NewDispatcher()
	defer func() { _ = clockSvc.Stop(ctx) }()
	clockSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		dispReg.Register(id, h)
	})

	eventsSvc := eventbus.NewDispatcher(bus, node)
	require.NoError(t, eventsSvc.Start(ctx))
	defer func() { _ = eventsSvc.Stop(context.Background()) }()
	eventsSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		dispReg.Register(id, h)
	})

	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "test-node")
	hostID := "test.pg:remote-leave-no-spurious"

	factory := func() (process.Process, error) {
		cfg := engine.FactoryConfig{
			Script: `
local time = require("time")

local function main()
    local scope = pg.open("test:pg")

    -- Subscribe to pg events (all groups)
    local sub, groups, err = scope:events()
    if err then
        return nil, "subscribe error: " .. tostring(err)
    end

    local ch = sub:channel()

    -- Join a local group to signal readiness to the Go test.
    local ok, err = scope:join("lua-ready-signal")
    if err then
        return nil, "join error: " .. tostring(err)
    end

    -- Consume the local join event for "lua-ready-signal"
    local timer = time.after(5000 * time.MILLISECOND)
    local result = channel.select{
        ch:case_receive(),
        timer:case_receive()
    }
    if result.channel == timer then
        return nil, "timeout waiting for lua-ready-signal join event"
    end

    -- Wait for remote join event for "watched-group"
    timer = time.after(5000 * time.MILLISECOND)
    result = channel.select{
        ch:case_receive(),
        timer:case_receive()
    }
    if result.channel == timer then
        return nil, "timeout waiting for remote join event"
    end

    local evt = result.value
    if evt.kind ~= "member.joined" then
        return nil, "expected member.joined for remote join, got " .. tostring(evt.kind)
    end
    if evt.path ~= "watched-group" then
        return nil, "expected path watched-group, got " .. tostring(evt.path)
    end

    -- Now wait for the remote leave events.
    -- We should get exactly ONE leave event for "watched-group",
    -- and NO spurious event for "unwatched-group".
    timer = time.after(5000 * time.MILLISECOND)
    result = channel.select{
        ch:case_receive(),
        timer:case_receive()
    }
    if result.channel == timer then
        return nil, "timeout waiting for remote leave event"
    end

    evt = result.value
    if evt.kind ~= "member.left" then
        return nil, "expected member.left, got " .. tostring(evt.kind)
    end
    if evt.path ~= "watched-group" then
        return nil, "expected leave for watched-group, got " .. tostring(evt.path)
    end

    -- Verify no spurious second event arrives
    timer = time.after(500 * time.MILLISECOND)
    result = channel.select{
        ch:case_receive(),
        timer:case_receive()
    }
    if result.channel ~= timer then
        -- Got a spurious event!
        local spurious = result.value
        return nil, "received spurious event: kind=" .. tostring(spurious.kind) .. " path=" .. tostring(spurious.path)
    end

    -- Clean up
    scope:leave("lua-ready-signal")

    -- Consume the local leave event
    timer = time.after(3000 * time.MILLISECOND)
    result = channel.select{
        ch:case_receive(),
        timer:case_receive()
    }

    sub:close()
    return "no_spurious_events"
end

return { main = main }
`,
			ScriptName: "pg_remote_leave_no_spurious_test",
			ModuleBinders: append(engine.CoreBinders(),
				func(l *lua.LState) error {
					engine.LoadModuleDef(l, pgmod.Module)
					return nil
				},
				func(l *lua.LState) error {
					mod, _ := timemod.Module.Build()
					l.SetGlobal("time", mod)
					return nil
				},
			),
		}
		f := engine.NewFactory(cfg)
		return f()
	}

	pool, err := inline.New(factory, dispReg)
	require.NoError(t, err)
	defer pool.Stop()

	err = node.RegisterHost(hostID, pool)
	require.NoError(t, err)

	// Run Lua in a goroutine; inject remote events from the main goroutine.
	type luaResult struct {
		result *runtime.Result
		err    error
	}
	luaDone := make(chan luaResult, 1)

	frameCtx, fc := ctxapi.OpenFrameContext(ctx)
	defer ctxapi.ReleaseFrameContext(fc)

	p := pidGen.Generate(hostID)
	err = runtime.SetFramePID(frameCtx, p)
	require.NoError(t, err)
	frameCtx = relay.WithNode(frameCtx, node)

	go func() {
		r, e := pool.Call(frameCtx, "main", nil)
		luaDone <- luaResult{r, e}
	}()

	// Wait for the Lua script to be ready (it joins "lua-ready-signal")
	require.Eventually(t, func() bool {
		members := service.GetMembers("lua-ready-signal")
		return len(members) > 0
	}, 5*time.Second, 20*time.Millisecond, "Lua script did not join lua-ready-signal in time")

	// Construct a remote PID from a fake node
	remotePID := &pid.PID{
		Node:   "remote-node",
		Host:   "remote-host",
		UniqID: "remote-proc-1",
	}
	remotePIDVal := remotePID.Precomputed()

	servicePIDFn := func(nodeID pid.NodeID) pid.PID {
		return pid.PID{
			Node: nodeID,
			Host: pid.HostID("pg"),
		}
	}

	// Inject remote join for "watched-group" only
	joinPkg := relay.NewPackage(
		servicePIDFn("remote-node"),
		servicePIDFn("test-node"),
		"pg.join",
		payload.New(map[string]any{
			"from":  "remote-node",
			"group": "watched-group",
			"pids":  []any{remotePIDVal.String()},
		}),
	)
	require.NoError(t, service.Send(joinPkg))
	time.Sleep(100 * time.Millisecond)

	// Inject remote leave for BOTH "watched-group" AND "unwatched-group".
	leavePkg := relay.NewPackage(
		servicePIDFn("remote-node"),
		servicePIDFn("test-node"),
		"pg.leave",
		payload.New(map[string]any{
			"from":   "remote-node",
			"pids":   []any{remotePIDVal.String()},
			"groups": []any{"watched-group", "unwatched-group"},
		}),
	)
	require.NoError(t, service.Send(leavePkg))

	// Wait for the Lua script to finish
	select {
	case lr := <-luaDone:
		require.NoError(t, lr.err)
		require.NotNil(t, lr.result)
		if lr.result.Error != nil {
			t.Fatalf("Lua execution error: %v", lr.result.Error)
		}
		val := lr.result.Value.Data()
		assert.Equal(t, "no_spurious_events", string(val.(lua.LString)))
	case <-time.After(15 * time.Second):
		t.Fatal("timeout waiting for Lua script to finish")
	}
}
