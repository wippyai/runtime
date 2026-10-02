// SPDX-License-Identifier: MPL-2.0

// Package replayer exposes Temporal's workflow history replayer to Lua.
package replayer

import (
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	tpropagator "github.com/wippyai/runtime/service/temporal/propagator"
	tworkflow "github.com/wippyai/runtime/service/temporal/workflow"
	"go.temporal.io/sdk/worker"
	sdkworkflow "go.temporal.io/sdk/workflow"
)

// Module is the replayer Lua module (ClassIO: usable from tests, not workflow bodies).
var Module = &luaapi.ModuleDef{
	Name:        "replayer",
	Description: "Temporal workflow history replay for determinism tests",
	Class:       []string{luaapi.ClassIO, luaapi.ClassNondeterministic},
	Build:       buildModule,
	Types:       ModuleTypes,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	mod := lua.CreateTable(0, 1)
	mod.RawSetString("replay_json_file", lua.LGoFunc(replayJSONFile))
	mod.Immutable = true
	return mod, nil
}

func invalidError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).WithKind(lua.Invalid).WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func internalError(l *lua.LState, goErr error, context string) int {
	err := lua.WrapErrorWithLua(l, goErr, context).WithKind(lua.Internal).WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

// replayJSONFile(workflow_id, history_json_path[, workflow_type_name]) -> (true) | (nil, err);
// nil err == deterministic replay. workflow_type_name overrides the registered type name
// when the workflow uses a custom meta name (defaults to workflow_id).
func replayJSONFile(l *lua.LState) int {
	workflowID := l.CheckString(1)
	path := l.CheckString(2)
	if workflowID == "" {
		return invalidError(l, "workflow id required")
	}
	if path == "" {
		return invalidError(l, "history file path required")
	}

	regID := registry.ParseID(workflowID)
	if regID.NS == "" || regID.Name == "" {
		return invalidError(l, "workflow id must be in 'namespace:name' form")
	}

	typeName := workflowID
	if tn := l.OptString(3, ""); tn != "" {
		typeName = tn
	}

	ctx := l.Context()

	dcReg := temporalapi.GetDataConverterRegistry(ctx)
	if dcReg == nil {
		return invalidError(l, "temporal data converter registry not available in context")
	}
	dc := dcReg.Build()

	opts := worker.WorkflowReplayerOptions{
		DataConverter:      dc,
		ContextPropagators: []sdkworkflow.ContextPropagator{tpropagator.New(dc)},
	}
	if wreg := temporalapi.GetWorkerInterceptorRegistry(ctx); wreg != nil {
		opts.Interceptors = wreg.GetAll()
	}

	r, err := worker.NewWorkflowReplayerWithOptions(opts)
	if err != nil {
		return internalError(l, err, "create workflow replayer")
	}

	// Register the runtime's dynamic definition (SDK accepts it as a WorkflowDefinitionFactory).
	factory := (&tworkflow.DefinitionFactory{ID: regID}).WithContext(ctx)
	r.RegisterWorkflowWithOptions(factory, sdkworkflow.RegisterOptions{Name: typeName})

	if err := r.ReplayWorkflowHistoryFromJSONFile(nil, path); err != nil {
		return internalError(l, err, "replay")
	}

	l.Push(lua.LBool(true))
	l.Push(lua.LNil)
	return 2
}
