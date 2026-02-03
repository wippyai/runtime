package registry

import (
	typio "github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// ID record type
var idType = typ.NewInterface("registry.ID", []typ.Method{
	{Name: "ns", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
	{Name: "name", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
})

// Entry type represents a registry entry
var entryType = typ.NewRecord().
	Field("id", typ.String).
	Field("kind", typ.String).
	Field("meta", typ.NewMap(typ.String, typ.Any)).
	Field("data", typ.Any).
	Build()

// Forward declarations for self-referential types
var (
	snapshotType *typ.Interface
	changesType  *typ.Interface
	versionType  *typ.Interface
	historyType  *typ.Interface
)

func init() {
	// Version type - define with all methods first (self-referential via previous/next)
	versionType = typ.NewInterface("registry.Version", []typ.Method{
		{Name: "id", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "previous", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(typ.Self)).Build()},
		{Name: "next", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(typ.Self)).Build()},
		{Name: "string", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
	})

	// Changes type (self-referential via create/update/delete, references versionType)
	changesType = typ.NewInterface("registry.Changes", []typ.Method{
		{Name: "ops", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewArray(typ.Any)).Build()},
		{Name: "create", Type: typ.Func().Param("self", typ.Self).Param("op", typ.Any).Returns(typ.Self, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "update", Type: typ.Func().Param("self", typ.Self).Param("op", typ.Any).Returns(typ.Self, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "delete", Type: typ.Func().Param("self", typ.Self).Param("op", typ.Any).Returns(typ.Self, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "apply", Type: typ.Func().Param("self", typ.Self).Returns(versionType, typ.NewOptional(typ.LuaError)).Build()},
	})

	// Snapshot type (references changesType and versionType)
	snapshotType = typ.NewInterface("registry.Snapshot", []typ.Method{
		{Name: "entries", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewArray(entryType)).Build()},
		{Name: "get", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Returns(entryType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "namespace", Type: typ.Func().Param("self", typ.Self).Param("ns", typ.String).Returns(typ.NewArray(entryType)).Build()},
		{Name: "find", Type: typ.Func().Param("self", typ.Self).Param("query", typ.Any).Returns(typ.NewArray(entryType)).Build()},
		{Name: "changes", Type: typ.Func().Param("self", typ.Self).Returns(changesType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "version", Type: typ.Func().Param("self", typ.Self).Returns(versionType).Build()},
	})

	// History type (references versionType and snapshotType)
	historyType = typ.NewInterface("registry.History", []typ.Method{
		{Name: "versions", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewArray(versionType), typ.NewOptional(typ.LuaError)).Build()},
		{Name: "get_version", Type: typ.Func().Param("self", typ.Self).Param("id", typ.Number).Returns(versionType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "snapshot_at", Type: typ.Func().Param("self", typ.Self).Param("version", versionType).Returns(snapshotType, typ.NewOptional(typ.LuaError)).Build()},
	})
}

// ModuleTypes returns the type manifest for the registry module.
func ModuleTypes() *typio.Manifest {
	m := typio.NewManifest("registry")

	m.DefineType("Snapshot", snapshotType)
	m.DefineType("Changes", changesType)
	m.DefineType("Version", versionType)
	m.DefineType("History", historyType)
	m.DefineType("ID", idType)
	m.DefineType("Entry", entryType)

	moduleType := typ.NewInterface("registry", []typ.Method{
		{Name: "get", Type: typ.Func().Param("key", typ.String).Returns(entryType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "find", Type: typ.Func().Param("query", typ.Any).Returns(typ.NewArray(entryType), typ.NewOptional(typ.LuaError)).Build()},
		{Name: "parse_id", Type: typ.Func().Param("id", typ.String).Returns(idType).Build()},
		{Name: "snapshot", Type: typ.Func().Returns(snapshotType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "snapshot_at", Type: typ.Func().Param("id", typ.Number).Returns(snapshotType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "current_version", Type: typ.Func().Returns(versionType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "versions", Type: typ.Func().Returns(typ.NewArray(versionType), typ.NewOptional(typ.LuaError)).Build()},
		{Name: "history", Type: typ.Func().Returns(historyType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "apply_version", Type: typ.Func().Param("version", versionType).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "build_delta", Type: typ.Func().Param("from", versionType).Param("to", versionType).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
