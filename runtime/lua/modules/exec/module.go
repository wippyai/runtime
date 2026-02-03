package exec

import (
	"sync"

	lua "github.com/wippyai/go-lua"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	execapi "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const (
	executorTypeName = "exec.Executor"
	processTypeName  = "exec.Process"
)

var (
	moduleTable *lua.LTable
	initOnce    sync.Once
)

func init() {
	value.RegisterTypeMethods(nil, executorTypeName, nil, executorMethods)
	value.RegisterTypeMethods(nil, processTypeName, nil, processMethods)
}

func initModuleTable() {
	mod := lua.CreateTable(0, 1)
	mod.RawSetString("get", lua.LGoFunc(execGet))
	mod.Immutable = true
	moduleTable = mod
}

// Module is the exec module definition.
var Module = &luaapi.ModuleDef{
	Name:        "exec",
	Description: "Command execution and process management",
	Class:       []string{luaapi.ClassIO, luaapi.ClassProcess, luaapi.ClassNondeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		initOnce.Do(initModuleTable)
		return moduleTable, []luaapi.YieldType{
			{Sample: &ProcessWaitYield{}, CmdID: execapi.ProcessWait},
		}
	},
	Types: ModuleTypes,
}
