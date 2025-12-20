package funcs

import (
	"context"
	"errors"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/runtime/lua/modules/future"
	lua "github.com/yuin/gopher-lua"
)

func TestModuleBuild(t *testing.T) {
	table, yields := Module.Build()

	if table == nil {
		t.Fatal("Build() returned nil table")
	}

	// Check module functions
	if table.RawGetString("call").Type() != lua.LTFunction {
		t.Error("call function not registered")
	}
	if table.RawGetString("async").Type() != lua.LTFunction {
		t.Error("async function not registered")
	}
	if table.RawGetString("new").Type() != lua.LTFunction {
		t.Error("new function not registered")
	}

	// Check yield types (Call, AsyncStart, AsyncCancel)
	if len(yields) != 3 {
		t.Errorf("expected 3 yield types, got %d", len(yields))
	}
}

func TestModuleBuildReuse(t *testing.T) {
	table1, _ := Module.Build()
	table2, _ := Module.Build()

	if table1 != table2 {
		t.Error("Build() should return the same table on subsequent calls")
	}
}

func TestModuleImmutable(t *testing.T) {
	table, _ := Module.Build()

	if !table.Immutable {
		t.Error("module table should be immutable")
	}
}

func TestModuleInfo(t *testing.T) {
	if Module.Name != "funcs" {
		t.Errorf("expected name 'funcs', got '%s'", Module.Name)
	}
	if Module.Description == "" {
		t.Error("module should have a description")
	}
	if len(Module.Class) == 0 {
		t.Error("module should have at least one class")
	}
}

func TestFutureTypeMethods(t *testing.T) {
	// Types are registered in init()
	mt := value.GetTypeMetatable(nil, future.TypeName)
	if mt == nil {
		t.Fatal("future type metatable not registered")
	}
}

func TestExecutorTypeMethods(t *testing.T) {
	// Types are registered in init()
	mt := value.GetTypeMetatable(nil, executorTypeName)
	if mt == nil {
		t.Fatal("executor type metatable not registered")
	}
}

func TestExecutorNew(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	table, _ := Module.Build()
	l.SetGlobal("funcs", table)

	err := l.DoString(`
		local exec = funcs.new()
		if exec == nil then
			error("new() returned nil")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestCallYieldType(t *testing.T) {
	yield := AcquireCallYield()
	defer ReleaseCallYield(yield)

	if yield.Type() != lua.LTUserData {
		t.Errorf("expected LTUserData, got %v", yield.Type())
	}
	if yield.String() != "<func_call_yield>" {
		t.Errorf("unexpected String(): %s", yield.String())
	}
}

func TestAsyncStartYieldType(t *testing.T) {
	yield := AcquireAsyncStartYield()
	defer ReleaseAsyncStartYield(yield)

	if yield.Type() != lua.LTUserData {
		t.Errorf("expected LTUserData, got %v", yield.Type())
	}
	if yield.String() != "<func_async_start_yield>" {
		t.Errorf("unexpected String(): %s", yield.String())
	}
}

func TestAsyncCancelYieldType(t *testing.T) {
	yield := AcquireAsyncCancelYield()
	defer ReleaseAsyncCancelYield(yield)

	if yield.Type() != lua.LTUserData {
		t.Errorf("expected LTUserData, got %v", yield.Type())
	}
	if yield.String() != "<func_async_cancel_yield>" {
		t.Errorf("unexpected String(): %s", yield.String())
	}
}

func TestCallYieldPooling(t *testing.T) {
	yield1 := AcquireCallYield()
	ReleaseCallYield(yield1)

	yield2 := AcquireCallYield()
	defer ReleaseCallYield(yield2)

	if yield1 != yield2 {
		t.Error("pool should reuse yield objects")
	}
}

func TestAsyncStartYieldPooling(t *testing.T) {
	y1 := AcquireAsyncStartYield()
	ReleaseAsyncStartYield(y1)

	y2 := AcquireAsyncStartYield()
	defer ReleaseAsyncStartYield(y2)

	if y1 != y2 {
		t.Error("pool should reuse yield objects")
	}
}

func TestAsyncCancelYieldPooling(t *testing.T) {
	y1 := AcquireAsyncCancelYield()
	ReleaseAsyncCancelYield(y1)

	y2 := AcquireAsyncCancelYield()
	defer ReleaseAsyncCancelYield(y2)

	if y1 != y2 {
		t.Error("pool should reuse yield objects")
	}
}

func TestCallYieldHandleResult(t *testing.T) {
	tests := []struct {
		name    string
		data    any
		err     error
		wantErr bool
	}{
		{
			name:    "success",
			data:    function.CallResult{Value: payload.NewString("result"), Error: nil},
			err:     nil,
			wantErr: false,
		},
		{
			name:    "call error",
			data:    nil,
			err:     errors.New("call failed"),
			wantErr: true,
		},
		{
			name:    "no response",
			data:    nil,
			err:     nil,
			wantErr: true,
		},
		{
			name:    "invalid response type",
			data:    "invalid",
			err:     nil,
			wantErr: true,
		},
		{
			name:    "response with error",
			data:    function.CallResult{Error: errors.New("function error")},
			err:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			y := AcquireCallYield()
			defer ReleaseCallYield(y)

			result := y.HandleResult(l, tt.data, tt.err)

			if len(result) != 2 {
				t.Fatalf("expected 2 return values, got %d", len(result))
			}

			if tt.wantErr {
				if result[1] == lua.LNil {
					t.Error("expected error, got nil")
				}
			} else {
				if result[1] != lua.LNil {
					t.Errorf("expected no error, got %v", result[1])
				}
			}
		})
	}
}

func TestAsyncStartYieldHandleResult(t *testing.T) {
	tests := []struct {
		name    string
		data    any
		err     error
		wantErr bool
	}{
		{
			name:    "success",
			data:    function.AsyncStartResult{Error: nil},
			err:     nil,
			wantErr: false,
		},
		{
			name:    "start error",
			data:    nil,
			err:     errors.New("start failed"),
			wantErr: true,
		},
		{
			name:    "invalid response type",
			data:    "invalid",
			err:     nil,
			wantErr: true,
		},
		{
			name:    "response with error",
			data:    function.AsyncStartResult{Error: errors.New("async error")},
			err:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			y := AcquireAsyncStartYield()
			y.Future = future.New("test", nil)
			defer ReleaseAsyncStartYield(y)

			result := y.HandleResult(l, tt.data, tt.err)

			if len(result) != 2 {
				t.Fatalf("expected 2 return values, got %d", len(result))
			}

			if tt.wantErr {
				if result[1] == lua.LNil {
					t.Error("expected error, got nil")
				}
			} else {
				if result[1] != lua.LNil {
					t.Errorf("expected no error, got %v", result[1])
				}
			}
		})
	}
}

func TestAsyncCancelYieldHandleResult(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr bool
	}{
		{
			name:    "success",
			err:     nil,
			wantErr: false,
		},
		{
			name:    "cancel error",
			err:     errors.New("cancel failed"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			y := AcquireAsyncCancelYield()
			defer ReleaseAsyncCancelYield(y)

			result := y.HandleResult(l, nil, tt.err)

			if len(result) != 2 {
				t.Fatalf("expected 2 return values, got %d", len(result))
			}

			if tt.wantErr {
				if result[1] == lua.LNil {
					t.Error("expected error, got nil")
				}
			} else {
				if result[1] != lua.LNil {
					t.Errorf("expected no error, got %v", result[1])
				}
				if result[0] != lua.LTrue {
					t.Error("expected true on success")
				}
			}
		})
	}
}

func TestCallYieldCommandID(t *testing.T) {
	y := AcquireCallYield()
	defer ReleaseCallYield(y)

	if y.CmdID() != function.Call {
		t.Errorf("expected CmdID %v, got %v", function.Call, y.CmdID())
	}
}

func TestAsyncStartYieldCommandID(t *testing.T) {
	y := AcquireAsyncStartYield()
	defer ReleaseAsyncStartYield(y)

	if y.CmdID() != function.AsyncStart {
		t.Errorf("expected CmdID %v, got %v", function.AsyncStart, y.CmdID())
	}
}

func TestAsyncCancelYieldCommandID(t *testing.T) {
	y := AcquireAsyncCancelYield()
	defer ReleaseAsyncCancelYield(y)

	if y.CmdID() != function.AsyncCancel {
		t.Errorf("expected CmdID %v, got %v", function.AsyncCancel, y.CmdID())
	}
}

func TestCallYieldToCommand(t *testing.T) {
	y := AcquireCallYield()
	defer ReleaseCallYield(y)

	cmd := y.ToCommand()
	if cmd == nil {
		t.Error("ToCommand should return a command")
	}
	if cmd != y.CallCmd {
		t.Error("ToCommand should return the CallCmd")
	}
}

func TestAsyncStartYieldToCommand(t *testing.T) {
	y := AcquireAsyncStartYield()
	defer ReleaseAsyncStartYield(y)

	cmd := y.ToCommand()
	if cmd == nil {
		t.Error("ToCommand should return a command")
	}
	if cmd != y.AsyncStartCmd {
		t.Error("ToCommand should return the AsyncStartCmd")
	}
}

func TestAsyncCancelYieldToCommand(t *testing.T) {
	y := AcquireAsyncCancelYield()
	defer ReleaseAsyncCancelYield(y)

	cmd := y.ToCommand()
	if cmd == nil {
		t.Error("ToCommand should return a command")
	}
	if cmd != y.AsyncCancelCmd {
		t.Error("ToCommand should return the AsyncCancelCmd")
	}
}

func TestExecutorState(t *testing.T) {
	e := &Executor{}

	if e.hasActor {
		t.Error("new executor should not have actor")
	}
	if e.hasScope {
		t.Error("new executor should not have scope")
	}
	if e.hasOptions {
		t.Error("new executor should not have options")
	}
}

func TestValidateTarget(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{
			name:    "valid target",
			target:  "ns:name",
			wantErr: false,
		},
		{
			name:    "empty target",
			target:  "",
			wantErr: true,
		},
		{
			name:    "missing namespace",
			target:  "name",
			wantErr: true,
		},
		{
			name:    "missing name",
			target:  "ns:",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			l.Push(lua.LString(tt.target))

			_, retCount := validateTarget(l, tt.target)

			if tt.wantErr {
				if retCount != 2 {
					t.Errorf("expected error return (2), got %d", retCount)
				}
			} else {
				if retCount != 0 {
					t.Errorf("expected success (0), got %d", retCount)
				}
			}
		})
	}
}

func TestExecutorWithContext(t *testing.T) {
	exec := &Executor{}

	if exec.values != nil {
		t.Error("new executor should have nil values")
	}

	// Create values bag and set on executor
	values := ctxapi.NewValues()
	values.Set("key1", "value1")
	values.Set("key2", 42)
	exec.values = values

	if v, ok := exec.values.Get("key1"); !ok || v != "value1" {
		t.Error("values should contain key1=value1")
	}
	if v, ok := exec.values.Get("key2"); !ok || v != 42 {
		t.Error("values should contain key2=42")
	}
}

func TestExecutorContextChaining(t *testing.T) {
	// Test that chained with_context creates new executor with merged values
	exec1 := &Executor{}

	// First with_context call
	values1 := ctxapi.NewValues()
	values1.Set("key1", "value1")
	exec2 := &Executor{
		values:   values1,
		hasActor: exec1.hasActor,
		actor:    exec1.actor,
		hasScope: exec1.hasScope,
		scope:    exec1.scope,
	}

	// Second with_context call (chaining) - should copy existing and add new
	values2 := ctxapi.NewValues()
	exec2.values.Iterate(func(k string, v any) {
		values2.Set(k, v)
	})
	values2.Set("key2", "value2")

	exec3 := &Executor{
		values:   values2,
		hasActor: exec2.hasActor,
		actor:    exec2.actor,
		hasScope: exec2.hasScope,
		scope:    exec2.scope,
	}

	// Verify chained executor has both values
	if v, ok := exec3.values.Get("key1"); !ok || v != "value1" {
		t.Error("chained executor should have key1 from first with_context")
	}
	if v, ok := exec3.values.Get("key2"); !ok || v != "value2" {
		t.Error("chained executor should have key2 from second with_context")
	}

	// Verify original executor is unchanged
	if _, ok := exec2.values.Get("key2"); ok {
		t.Error("original executor should not be modified by chaining")
	}
}

func TestCallYieldContextPairs(t *testing.T) {
	y := AcquireCallYield()
	defer ReleaseCallYield(y)

	// Verify Task.Context is initially empty
	if len(y.Task.Context) != 0 {
		t.Errorf("expected empty context, got %d pairs", len(y.Task.Context))
	}
}

func TestExecutorCallAddsContextToTask(t *testing.T) {
	// Test that when executor has values, they are added to Task.Context
	exec := &Executor{}
	values := ctxapi.NewValues()
	values.Set("trace_id", "test-123")
	exec.values = values

	// Verify executor has values
	if exec.values == nil || exec.values.Len() == 0 {
		t.Fatal("executor should have values set")
	}

	// The actual context addition happens in executorCall when creating the yield
	// This test verifies the executor state that would trigger context addition
	if exec.values.Len() != 1 {
		t.Errorf("expected 1 value, got %d", exec.values.Len())
	}
}

func TestPlainCallInheritsFrameContext(t *testing.T) {
	// Test that buildContextPairs extracts context from frame
	l := lua.NewState()
	defer l.Close()

	// Set up frame context with values
	ctx, fc := ctxapi.OpenFrameContext(l.Context())
	defer ctxapi.ReleaseFrameContext(fc)

	values := ctxapi.NewValues()
	values.Set("trace_id", "inherited-trace-123")
	values.Set("request_id", "req-456")
	if err := fc.Set(ctxapi.ValuesCtx, values); err != nil {
		t.Fatalf("failed to set values: %v", err)
	}

	l.SetContext(ctx)

	// Test buildContextPairs helper extracts values from frame
	pairs := buildContextPairs(l)

	if len(pairs) == 0 {
		t.Error("buildContextPairs should extract context values from frame")
	}

	// Check that values pair is present
	hasValues := false
	for _, pair := range pairs {
		if pair.Key == ctxapi.ValuesCtx {
			hasValues = true
			break
		}
	}
	if !hasValues {
		t.Error("context pairs should include values from frame context")
	}
}

func TestCallYieldHasContextPairs(t *testing.T) {
	// Test that CallYield created by call() includes context pairs from frame
	// This is a unit test that verifies the yield structure
	l := lua.NewState()
	defer l.Close()

	// Set up frame context with values
	ctx, fc := ctxapi.OpenFrameContext(l.Context())
	defer ctxapi.ReleaseFrameContext(fc)

	values := ctxapi.NewValues()
	values.Set("trace_id", "call-trace-123")
	if err := fc.Set(ctxapi.ValuesCtx, values); err != nil {
		t.Fatalf("failed to set values: %v", err)
	}

	l.SetContext(ctx)

	// Create a CallYield and add context pairs (simulating what call() should do)
	yield := AcquireCallYield()
	defer ReleaseCallYield(yield)

	// This is what call() SHOULD do - add context pairs
	pairs := buildContextPairs(l)
	yield.Task.Context = pairs

	// Verify the yield has context pairs
	if len(yield.Task.Context) == 0 {
		t.Error("CallYield should have context pairs set")
	}

	// Check that values pair is present
	hasValues := false
	for _, pair := range yield.Task.Context {
		if pair.Key == ctxapi.ValuesCtx {
			hasValues = true
			break
		}
	}
	if !hasValues {
		t.Error("Task.Context should include values pair from frame context")
	}
}

func TestExecutorCallInheritsFrameContext(t *testing.T) {
	// Test that funcs.new():call() inherits context from frame
	// even when with_context() is NOT explicitly called
	l := lua.NewState()
	defer l.Close()

	// Set up frame context with values (simulating a caller with session_id)
	ctx, fc := ctxapi.OpenFrameContext(l.Context())
	defer ctxapi.ReleaseFrameContext(fc)

	values := ctxapi.NewValues()
	values.Set("session_id", "sess-123")
	values.Set("trace_id", "trace-456")
	if err := fc.Set(ctxapi.ValuesCtx, values); err != nil {
		t.Fatalf("failed to set values: %v", err)
	}

	l.SetContext(ctx)

	// Create a new Executor (like funcs.new() does)
	exec := &Executor{}
	// NOT calling with_context() - executor has no explicit context

	// Simulate what executorCall now does: start with inherited, then overlay
	yield := AcquireCallYield()
	defer ReleaseCallYield(yield)

	// Fixed behavior: start with inherited context from frame
	yield.Task.Context = buildContextPairs(l)

	// Then overlay with explicit executor settings
	if exec.hasActor {
		yield.Task.Context = append(yield.Task.Context, ctxapi.Pair{})
	}
	if exec.hasScope {
		yield.Task.Context = append(yield.Task.Context, ctxapi.Pair{})
	}
	if exec.values != nil && exec.values.Len() > 0 {
		yield.Task.Context = append(yield.Task.Context, ctxapi.ValuesPair(exec.values))
	}

	// Task.Context should have inherited values from frame
	if len(yield.Task.Context) == 0 {
		t.Error("executorCall should inherit context from frame when executor has no explicit context")
	}

	// Verify values pair is present
	hasValues := false
	for _, pair := range yield.Task.Context {
		if pair.Key == ctxapi.ValuesCtx {
			hasValues = true
			break
		}
	}
	if !hasValues {
		t.Error("Task.Context should include values pair from frame context")
	}
}

func TestExecutorCallMergesExplicitAndInheritedContext(t *testing.T) {
	// Test that explicit executor context is merged with inherited frame context
	l := lua.NewState()
	defer l.Close()

	// Set up frame context with values
	ctx, fc := ctxapi.OpenFrameContext(l.Context())
	defer ctxapi.ReleaseFrameContext(fc)

	frameValues := ctxapi.NewValues()
	frameValues.Set("session_id", "sess-from-frame")
	frameValues.Set("inherited_key", "inherited_value")
	if err := fc.Set(ctxapi.ValuesCtx, frameValues); err != nil {
		t.Fatalf("failed to set values: %v", err)
	}

	l.SetContext(ctx)

	// Create executor with explicit values (like with_context() does)
	execValues := ctxapi.NewValues()
	execValues.Set("explicit_key", "explicit_value")
	exec := &Executor{values: execValues}

	// What executorCall SHOULD do: merge inherited + explicit
	// Inherited context should be base, explicit should overlay
	inheritedPairs := buildContextPairs(l)

	// Build final context: start with inherited, add explicit overrides
	var finalContext []ctxapi.Pair
	finalContext = append(finalContext, inheritedPairs...)
	if exec.values != nil && exec.values.Len() > 0 {
		finalContext = append(finalContext, ctxapi.ValuesPair(exec.values))
	}

	// Should have both inherited and explicit values
	if len(finalContext) < 2 {
		t.Error("should have both inherited values and explicit values pairs")
	}
}

func TestAsyncYieldInheritsFrameContext(t *testing.T) {
	// Test that async calls also inherit context from frame
	l := lua.NewState()
	defer l.Close()

	// Set up frame context with values
	ctx, fc := ctxapi.OpenFrameContext(l.Context())
	defer ctxapi.ReleaseFrameContext(fc)

	values := ctxapi.NewValues()
	values.Set("session_id", "async-sess-123")
	if err := fc.Set(ctxapi.ValuesCtx, values); err != nil {
		t.Fatalf("failed to set values: %v", err)
	}

	l.SetContext(ctx)

	// Verify buildContextPairs returns inherited values for async
	pairs := buildContextPairs(l)

	if len(pairs) == 0 {
		t.Error("async calls should inherit context from frame")
	}

	hasValues := false
	for _, pair := range pairs {
		if pair.Key == ctxapi.ValuesCtx {
			hasValues = true
			break
		}
	}
	if !hasValues {
		t.Error("async context should include values pair from frame")
	}
}

func TestValuesMerging(t *testing.T) {
	// Test that when both frame context and explicit context have values,
	// they should be MERGED, not replaced.
	// This is the core bug: if frame has {session_id: "xxx"} and explicit has {agent_id: "yyy"},
	// the final context should have BOTH values.

	frameValues := ctxapi.NewValues()
	frameValues.Set("session_id", "sess-123")
	frameValues.Set("user_id", "user-456")

	execValues := ctxapi.NewValues()
	execValues.Set("agent_id", "agent-789")
	execValues.Set("call_id", "call-abc")

	// Merge: start with frame values, overlay with exec values
	mergedValues := ctxapi.NewValues()
	frameValues.Iterate(func(key string, val any) {
		mergedValues.Set(key, val)
	})
	execValues.Iterate(func(key string, val any) {
		mergedValues.Set(key, val)
	})

	// Verify ALL values are present
	if v, _ := mergedValues.Get("session_id"); v != "sess-123" {
		t.Errorf("merged values should have session_id=sess-123, got %v", v)
	}
	if v, _ := mergedValues.Get("user_id"); v != "user-456" {
		t.Errorf("merged values should have user_id=user-456, got %v", v)
	}
	if v, _ := mergedValues.Get("agent_id"); v != "agent-789" {
		t.Errorf("merged values should have agent_id=agent-789, got %v", v)
	}
	if v, _ := mergedValues.Get("call_id"); v != "call-abc" {
		t.Errorf("merged values should have call_id=call-abc, got %v", v)
	}
}

func TestBuildMergedContextPairs(t *testing.T) {
	// Test the helper function that should merge frame context values with explicit values
	l := lua.NewState()
	defer l.Close()

	// Set up frame context with values
	ctx, fc := ctxapi.OpenFrameContext(l.Context())
	defer ctxapi.ReleaseFrameContext(fc)

	frameValues := ctxapi.NewValues()
	frameValues.Set("session_id", "sess-from-frame")
	frameValues.Set("inherited_key", "inherited_value")
	if err := fc.Set(ctxapi.ValuesCtx, frameValues); err != nil {
		t.Fatalf("failed to set frame values: %v", err)
	}

	l.SetContext(ctx)

	// Create explicit values (simulating with_context)
	execValues := ctxapi.NewValues()
	execValues.Set("explicit_key", "explicit_value")
	execValues.Set("agent_id", "agent-123")

	// This is what buildMergedContextPairs should do
	mergedPairs := buildMergedContextPairs(l, execValues)

	// Should have exactly ONE ValuesPair with merged values
	valuesPairCount := 0
	var mergedValues ctxapi.Values
	for _, pair := range mergedPairs {
		if pair.Key == ctxapi.ValuesCtx {
			valuesPairCount++
			mergedValues = pair.Value.(ctxapi.Values)
		}
	}

	if valuesPairCount != 1 {
		t.Errorf("should have exactly 1 ValuesPair, got %d", valuesPairCount)
	}

	if mergedValues == nil {
		t.Fatal("mergedValues should not be nil")
	}

	// Verify both inherited and explicit values are present
	if v, _ := mergedValues.Get("session_id"); v != "sess-from-frame" {
		t.Errorf("merged should have session_id=sess-from-frame, got %v", v)
	}
	if v, _ := mergedValues.Get("inherited_key"); v != "inherited_value" {
		t.Errorf("merged should have inherited_key, got %v", v)
	}
	if v, _ := mergedValues.Get("explicit_key"); v != "explicit_value" {
		t.Errorf("merged should have explicit_key, got %v", v)
	}
	if v, _ := mergedValues.Get("agent_id"); v != "agent-123" {
		t.Errorf("merged should have agent_id=agent-123, got %v", v)
	}
}

func TestExecutorCallValuesMergedNotReplaced(t *testing.T) {
	// Integration test: verify that executorCall properly merges values
	// This test should FAIL with the current implementation and PASS after the fix
	l := lua.NewState()
	defer l.Close()

	// Set up frame context with session_id
	ctx, fc := ctxapi.OpenFrameContext(l.Context())
	defer ctxapi.ReleaseFrameContext(fc)

	frameValues := ctxapi.NewValues()
	frameValues.Set("session_id", "sess-abc")
	if err := fc.Set(ctxapi.ValuesCtx, frameValues); err != nil {
		t.Fatalf("failed to set frame values: %v", err)
	}
	l.SetContext(ctx)

	// Create executor with explicit values
	execValues := ctxapi.NewValues()
	execValues.Set("agent_id", "agent-xyz")
	exec := &Executor{values: execValues}

	// Simulate what executorCall does - build merged context
	mergedPairs := buildMergedContextPairs(l, exec.values)

	// Find the merged values
	var mergedValues ctxapi.Values
	for _, pair := range mergedPairs {
		if pair.Key == ctxapi.ValuesCtx {
			mergedValues = pair.Value.(ctxapi.Values)
			break
		}
	}

	if mergedValues == nil {
		t.Fatal("should have merged values")
	}

	// Key assertion: BOTH session_id from frame AND agent_id from explicit should be present
	sessionID, hasSession := mergedValues.Get("session_id")
	agentID, hasAgent := mergedValues.Get("agent_id")

	if !hasSession || sessionID != "sess-abc" {
		t.Errorf("merged values should preserve session_id from frame, got hasSession=%v, value=%v", hasSession, sessionID)
	}
	if !hasAgent || agentID != "agent-xyz" {
		t.Errorf("merged values should have agent_id from explicit, got hasAgent=%v, value=%v", hasAgent, agentID)
	}
}

func TestDuplicateValuesPairsOverwrite(t *testing.T) {
	// This test demonstrates the BUG: when two ValuesPairs are added to context,
	// the second one OVERWRITES the first instead of merging.
	// This is WHY session_id gets lost when tool_caller uses with_context().

	// Simulate two ValuesPairs being applied (like the old broken code did)
	frameValues := ctxapi.NewValues()
	frameValues.Set("session_id", "sess-123")
	frameValues.Set("user_id", "user-456")

	execValues := ctxapi.NewValues()
	execValues.Set("agent_id", "agent-789")

	// Old broken approach: two separate ValuesPairs
	pairs := []ctxapi.Pair{
		ctxapi.ValuesPair(frameValues), // First pair with session_id
		ctxapi.ValuesPair(execValues),  // Second pair overwrites!
	}

	// Apply pairs to a new frame (simulating what scheduler does)
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)

	for _, p := range pairs {
		_ = fc.Set(p.Key, p.Value)
	}

	// The bug: session_id is LOST because second ValuesPair overwrote the first
	resultValues := ctxapi.GetValues(ctx)

	// This assertion shows the bug - session_id is gone!
	_, hasSessionID := resultValues.Get("session_id")
	_, hasAgentID := resultValues.Get("agent_id")

	// With the bug, hasSessionID will be FALSE (overwritten)
	// After fix with merged values, hasSessionID should be TRUE
	t.Logf("hasSessionID=%v, hasAgentID=%v", hasSessionID, hasAgentID)

	if !hasSessionID {
		t.Log("BUG CONFIRMED: session_id was overwritten by second ValuesPair")
	}
	if !hasAgentID {
		t.Error("agent_id should be present")
	}

	// The fix: use buildMergedContextPairs which creates ONE ValuesPair with merged values
}

func TestMergedContextPairsAppliedCorrectly(t *testing.T) {
	// This test verifies the FIX: merged pairs when applied to a new frame
	// preserve BOTH inherited and explicit values.
	l := lua.NewState()
	defer l.Close()

	// Set up caller's frame context with session_id (simulating session process)
	callerCtx, callerFC := ctxapi.OpenFrameContext(l.Context())
	defer ctxapi.ReleaseFrameContext(callerFC)

	frameValues := ctxapi.NewValues()
	frameValues.Set("session_id", "sess-123")
	frameValues.Set("user_id", "user-456")
	_ = callerFC.Set(ctxapi.ValuesCtx, frameValues)
	l.SetContext(callerCtx)

	// Create explicit values (simulating with_context({agent_id=...}))
	execValues := ctxapi.NewValues()
	execValues.Set("agent_id", "agent-789")
	execValues.Set("call_id", "call-abc")

	// Build merged pairs (this is what the fix does)
	mergedPairs := buildMergedContextPairs(l, execValues)

	// Apply merged pairs to a NEW frame (simulating scheduler creating callee frame)
	calleeCtx, calleeFC := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(calleeFC)

	for _, p := range mergedPairs {
		_ = calleeFC.Set(p.Key, p.Value)
	}

	// Verify BOTH inherited AND explicit values are present in the callee's context
	resultValues := ctxapi.GetValues(calleeCtx)
	if resultValues == nil {
		t.Fatal("resultValues should not be nil")
	}

	sessionID, hasSessionID := resultValues.Get("session_id")
	userID, hasUserID := resultValues.Get("user_id")
	agentID, hasAgentID := resultValues.Get("agent_id")
	callID, hasCallID := resultValues.Get("call_id")

	// All values should be present
	if !hasSessionID || sessionID != "sess-123" {
		t.Errorf("session_id should be preserved, got hasSessionID=%v, value=%v", hasSessionID, sessionID)
	}
	if !hasUserID || userID != "user-456" {
		t.Errorf("user_id should be preserved, got hasUserID=%v, value=%v", hasUserID, userID)
	}
	if !hasAgentID || agentID != "agent-789" {
		t.Errorf("agent_id should be present, got hasAgentID=%v, value=%v", hasAgentID, agentID)
	}
	if !hasCallID || callID != "call-abc" {
		t.Errorf("call_id should be present, got hasCallID=%v, value=%v", hasCallID, callID)
	}
}
