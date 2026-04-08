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
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/runtime/lua/engine"
	pgmod "github.com/wippyai/runtime/runtime/lua/modules/pg"
	"github.com/wippyai/runtime/system/eventbus"
	syspg "github.com/wippyai/runtime/system/pg"
	sysrelay "github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"go.uber.org/zap"
)

// noopTopology is a minimal topology mock for integration tests.
// It satisfies the topology.Topology interface but does nothing.
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

	// Create pg service with noop topology (required for monitoring on join)
	topo := &noopTopology{}
	service := syspg.NewService(logger, node, topo, nil, bus, "test-node")
	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	ctx = relay.WithNode(ctx, node)

	require.NoError(t, service.Start(ctx))

	// Create dispatcher registry and register pg commands
	reg := scheduler.NewRegistry()
	pgDisp := syspg.NewDispatcher(service, node, logger)
	pgDisp.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		reg.Register(id, h)
	})

	ts := &pgTestScheduler{
		pending: make(map[string]chan *runtime.Result),
	}
	opts := []actor.Option{
		actor.WithWorkers(4),
		actor.WithLifecycle(ts),
	}
	ts.Scheduler = actor.NewScheduler(reg, opts...)
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

// --- Integration Tests ---

func TestIntegration_JoinAndGetMembers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	testPID := uniquePGTestPID()

	// Join a group
	result := runPGScriptWithPID(t, tc, testPID, `
		local ok, err = pg.join("workers")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LTrue, result.Value.Data())

	// Get members and verify the PID is in the group
	result = runPGScriptWithPID(t, tc, testPID, `
		local members, err = pg.get_members("workers")
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
		local ok, err = pg.join("temp-group")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)

	// Verify membership
	result = runPGScriptWithPID(t, tc, testPID, `
		local members, err = pg.get_members("temp-group")
		if err then return nil, tostring(err) end
		return #members
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(1), result.Value.Data())

	// Leave
	result = runPGScriptWithPID(t, tc, testPID, `
		local ok, err = pg.leave("temp-group")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LTrue, result.Value.Data())

	// Verify group is empty
	result = runPGScriptWithPID(t, tc, testPID, `
		local members, err = pg.get_members("temp-group")
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
		local ok, err = pg.leave("nonexistent-group")
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
		local members, err = pg.get_members("empty-group")
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
		local ok, err = pg.join("local-group")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)

	// Get local members
	result = runPGScriptWithPID(t, tc, testPID, `
		local members, err = pg.get_local_members("local-group")
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
		local ok, err = pg.join("group-a")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)

	result = runPGScriptWithPID(t, tc, testPID, `
		local ok, err = pg.join("group-b")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)

	// which_groups should return both
	result = runPGScriptWithPID(t, tc, testPID, `
		local groups, err = pg.which_groups()
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
		local groups, err = pg.which_groups()
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
			local ok, err = pg.join("multi-group")
			if err then return nil, tostring(err) end
			return ok
		`)
		require.NoError(t, result.Error)
	}

	// Verify all three are members
	result := runPGScriptWithPID(t, tc, pid1, `
		local members, err = pg.get_members("multi-group")
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
			local ok, err = pg.join("`+group+`")
			if err then return nil, tostring(err) end
			return ok
		`)
		require.NoError(t, result.Error)
	}

	// which_groups should return 3
	result := runPGScriptWithPID(t, tc, testPID, `
		local groups, err = pg.which_groups()
		if err then return nil, tostring(err) end
		return #groups
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(3), result.Value.Data())

	// Each group should have 1 member
	for _, group := range []string{"alpha", "beta", "gamma"} {
		result := runPGScriptWithPID(t, tc, testPID, `
			local members, err = pg.get_members("`+group+`")
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
		local ok, err = pg.join("pid-check")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)

	// Verify members returns string PIDs
	result = runPGScriptWithPID(t, tc, testPID, `
		local members, err = pg.get_members("pid-check")
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
		local ok, err = pg.join("broadcast-group")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)

	// Broadcast locally - should succeed even though the message
	// won't be received (no listener in this simple test)
	result = runPGScriptWithPID(t, tc, testPID, `
		local ok, err = pg.broadcast_local("broadcast-group", "test.topic", "hello")
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
		local ok, err = pg.join("bcast-group")
		if err then return nil, tostring(err) end
		return ok
	`)
	require.NoError(t, result.Error)

	// Broadcast globally
	result = runPGScriptWithPID(t, tc, testPID, `
		local ok, err = pg.broadcast("bcast-group", "hello.topic", "world")
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
		local ok, err = pg.broadcast("empty-bcast", "topic", "data")
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
		-- Join
		local ok, err = pg.join("lifecycle")
		if err then return nil, "join: " .. tostring(err) end

		-- Check members
		local members, err = pg.get_members("lifecycle")
		if err then return nil, "get_members: " .. tostring(err) end
		if #members ~= 1 then
			return nil, "expected 1 member, got " .. #members
		end

		-- Check which_groups
		local groups, err = pg.which_groups()
		if err then return nil, "which_groups: " .. tostring(err) end
		if #groups < 1 then
			return nil, "expected at least 1 group, got " .. #groups
		end

		-- Leave
		local ok, err = pg.leave("lifecycle")
		if err then return nil, "leave: " .. tostring(err) end

		-- Verify empty
		local members2, err = pg.get_members("lifecycle")
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
				local ok, err = pg.join("concurrent-group")
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
		local members, err = pg.get_members("concurrent-group")
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
			local ok, err = pg.join("multi-join")
			if err then return nil, tostring(err) end
			return ok
		`)
		require.NoError(t, result.Error)
	}

	// The PID should appear 3 times in the member list
	result := runPGScriptWithPID(t, tc, testPID, `
		local members, err = pg.get_members("multi-join")
		if err then return nil, tostring(err) end
		return #members
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(3), result.Value.Data())

	// Leave once - should reduce count by 1
	result = runPGScriptWithPID(t, tc, testPID, `
		local ok, err = pg.leave("multi-join")
		if err then return nil, tostring(err) end
		local members, err = pg.get_members("multi-join")
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
		local ok, err = pg.join("leave-zero")
		if err then return nil, "join1: " .. tostring(err) end

		ok, err = pg.join("leave-zero")
		if err then return nil, "join2: " .. tostring(err) end

		ok, err = pg.leave("leave-zero")
		if err then return nil, "leave1: " .. tostring(err) end

		ok, err = pg.leave("leave-zero")
		if err then return nil, "leave2: " .. tostring(err) end

		local members, err = pg.get_members("leave-zero")
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
		local ok, err = pg.join("leave-extra")
		if err then return nil, "join: " .. tostring(err) end

		ok, err = pg.leave("leave-extra")
		if err then return nil, "leave1: " .. tostring(err) end

		ok, err = pg.leave("leave-extra")
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
			local ok, err = pg.join("bcast-multi")
			if err then return nil, tostring(err) end
			return ok
		`)
		require.NoError(t, result.Error)
	}

	// Broadcast from one of them
	result := runPGScriptWithPID(t, tc, pid1, `
		local ok, err = pg.broadcast("bcast-multi", "notify", "hello", "extra-payload")
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
		local ok, err = pg.broadcast_local("empty-local", "topic", "data")
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
		local members, err = pg.get_local_members("nonexistent-local")
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
		local ok, err = pg.join("rejoin")
		if err then return nil, "join1: " .. tostring(err) end

		ok, err = pg.leave("rejoin")
		if err then return nil, "leave: " .. tostring(err) end

		-- Verify empty
		local members, err = pg.get_members("rejoin")
		if err then return nil, "check1: " .. tostring(err) end
		if #members ~= 0 then
			return nil, "expected 0 after leave, got " .. #members
		end

		-- Rejoin
		ok, err = pg.join("rejoin")
		if err then return nil, "join2: " .. tostring(err) end

		members, err = pg.get_members("rejoin")
		if err then return nil, "check2: " .. tostring(err) end
		return #members
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(1), result.Value.Data())
}

func TestIntegration_WhichGroupsAfterLeave(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	testPID := uniquePGTestPID()

	// Join two groups, leave one, verify which_groups
	result := runPGScriptWithPID(t, tc, testPID, `
		local ok, err = pg.join("wg-stay")
		if err then return nil, "join1: " .. tostring(err) end

		ok, err = pg.join("wg-leave")
		if err then return nil, "join2: " .. tostring(err) end

		-- Should have 2 groups
		local groups, err = pg.which_groups()
		if err then return nil, "wg1: " .. tostring(err) end
		if #groups ~= 2 then
			return nil, "expected 2 groups, got " .. #groups
		end

		-- Leave one
		ok, err = pg.leave("wg-leave")
		if err then return nil, "leave: " .. tostring(err) end

		-- Should have 1 group
		groups, err = pg.which_groups()
		if err then return nil, "wg2: " .. tostring(err) end
		return #groups
	`)
	require.NoError(t, result.Error)
	assert.Equal(t, lua.LNumber(1), result.Value.Data())
}
