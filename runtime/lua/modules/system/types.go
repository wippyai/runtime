// SPDX-License-Identifier: MPL-2.0

package system

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// MemStats type
var memStatsType = typ.NewRecord().
	Field("alloc", typ.Number).
	Field("total_alloc", typ.Number).
	Field("sys", typ.Number).
	Field("heap_alloc", typ.Number).
	Field("heap_sys", typ.Number).
	Field("heap_idle", typ.Number).
	Field("heap_in_use", typ.Number).
	Field("heap_released", typ.Number).
	Field("heap_objects", typ.Number).
	Field("stack_in_use", typ.Number).
	Field("stack_sys", typ.Number).
	Field("mspan_in_use", typ.Number).
	Field("mspan_sys", typ.Number).
	Field("num_gc", typ.Number).
	Field("next_gc", typ.Number).
	Build()

// ModuleInfo type
var moduleInfoType = typ.NewRecord().
	Field("name", typ.String).
	Field("description", typ.String).
	Field("class", typ.NewArray(typ.String)).
	Build()

// memory submodule type
var memoryType = typ.NewInterface("system.memory", []typ.Method{
	{Name: "stats", Type: typ.Func().Returns(memStatsType, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "allocated", Type: typ.Func().Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "heap_objects", Type: typ.Func().Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "set_limit", Type: typ.Func().Param("limit", typ.Number).Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "get_limit", Type: typ.Func().Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
})

// gc submodule type
var gcType = typ.NewInterface("system.gc", []typ.Method{
	{Name: "collect", Type: typ.Func().Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "set_percent", Type: typ.Func().Param("percent", typ.Number).Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "get_percent", Type: typ.Func().Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
})

// runtime submodule type
var runtimeType = typ.NewInterface("system.runtime", []typ.Method{
	{Name: "goroutines", Type: typ.Func().Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "max_procs", Type: typ.Func().OptParam("procs", typ.Number).Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "cpu_count", Type: typ.Func().Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
})

// process submodule type
var processSubType = typ.NewInterface("system.process", []typ.Method{
	{Name: "pid", Type: typ.Func().Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "hostname", Type: typ.Func().Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "cwd", Type: typ.Func().Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
})

// HostStats type
var hostStatsType = typ.NewRecord().
	Field("id", typ.String).
	Field("workers", typ.Number).
	Field("processes", typ.Number).
	Field("executed", typ.Number).
	Field("stolen", typ.Number).
	Field("queue_depth", typ.Number).
	Build()

// ProcessStats type
var processStatsType = typ.NewRecord().
	Field("pid", typ.String).
	Field("host", typ.String).
	Field("source", typ.String).
	Field("state", typ.String).
	Field("steps", typ.Number).
	Field("started_at", typ.Number).
	Field("parent", typ.NewOptional(typ.String)).
	Field("actor_id", typ.NewOptional(typ.String)).
	Field("stats", typ.NewOptional(typ.NewMap(typ.String, typ.Any))).
	Build()

// hosts submodule type
var hostsType = typ.NewInterface("system.hosts", []typ.Method{
	{Name: "list", Type: typ.Func().Returns(typ.NewArray(hostStatsType), typ.NewOptional(typ.LuaError)).Build()},
	{Name: "processes", Type: typ.Func().Param("host_id", typ.String).Returns(typ.NewArray(processStatsType), typ.NewOptional(typ.LuaError)).Build()},
})

// serviceStateType describes one supervised service's state, matching the
// table supervisorState/supervisorStates build.
var serviceStateType = typ.NewRecord().
	Field("id", typ.String).
	Field("status", typ.String).
	Field("desired", typ.String).
	Field("retry_count", typ.Number).
	Field("last_update", typ.Number).
	Field("started_at", typ.Number).
	Field("details", typ.NewOptional(typ.String)).
	Build()

// supervisor submodule type
var supervisorType = typ.NewInterface("system.supervisor", []typ.Method{
	{Name: "state", Type: typ.Func().Param("service_id", typ.String).Returns(serviceStateType, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "states", Type: typ.Func().Returns(typ.NewArray(serviceStateType), typ.NewOptional(typ.LuaError)).Build()},
})

// NodeInfo type describing a cluster member.
var nodeInfoType = typ.NewRecord().
	Field("id", typ.String).
	Field("is_local", typ.Boolean).
	Field("addr", typ.NewOptional(typ.String)).
	Field("meta", typ.NewOptional(typ.NewMap(typ.String, typ.String))).
	Build()

// node submodule type
var nodeType = typ.NewInterface("system.node", []typ.Method{
	{Name: "id", Type: typ.Func().Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "addr", Type: typ.Func().Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "role", Type: typ.Func().Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
})

// cluster submodule type
var clusterType = typ.NewInterface("system.cluster", []typ.Method{
	{Name: "members", Type: typ.Func().Returns(typ.NewArray(nodeInfoType), typ.NewOptional(typ.LuaError)).Build()},
	{Name: "leader", Type: typ.Func().Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "size", Type: typ.Func().Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
})

// raft submodule type
var raftType = typ.NewInterface("system.raft", []typ.Method{
	{Name: "is_leader", Type: typ.Func().Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "is_member", Type: typ.Func().Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "role", Type: typ.Func().Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "term", Type: typ.Func().Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "commit_index", Type: typ.Func().Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "stats", Type: typ.Func().Returns(typ.NewMap(typ.String, typ.String), typ.NewOptional(typ.LuaError)).Build()},
})

// lock submodule type
var lockType = typ.NewInterface("system.lock", []typ.Method{
	{Name: "acquire", Type: typ.Func().Param("name", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "release", Type: typ.Func().Param("name", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
})

// Methods interface
var systemMethodsType = typ.NewInterface("system", []typ.Method{
	{Name: "exit", Type: typ.Func().OptParam("code", typ.Number).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "modules", Type: typ.Func().Returns(typ.NewArray(moduleInfoType), typ.NewOptional(typ.LuaError)).Build()},
})

// ModuleTypes returns the type manifest for the system module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("system")

	m.DefineType("MemStats", memStatsType)
	m.DefineType("ModuleInfo", moduleInfoType)
	m.DefineType("HostStats", hostStatsType)
	m.DefineType("ProcessStats", processStatsType)
	m.DefineType("NodeInfo", nodeInfoType)

	// Submodule fields
	submodulesType := typ.NewRecord().
		Field("memory", memoryType).
		Field("gc", gcType).
		Field("runtime", runtimeType).
		Field("process", processSubType).
		Field("supervisor", supervisorType).
		Field("hosts", hostsType).
		Field("node", nodeType).
		Field("cluster", clusterType).
		Field("raft", raftType).
		Field("lock", lockType).
		Build()

	// Combine methods and fields via intersection
	m.SetExport(typ.NewIntersection(systemMethodsType, submodulesType))
	return m
}
