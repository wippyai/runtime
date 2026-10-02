// SPDX-License-Identifier: MPL-2.0

package eval

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

var (
	compileModeType = typ.NewUnion(
		typ.LiteralString("lite"),
		typ.LiteralString("typed"),
		typ.LiteralString("jit"),
	)
	sendModeType = typ.NewUnion(
		typ.LiteralString("cap"),
		typ.LiteralString("object_cap"),
		typ.LiteralString("object-capability"),
		typ.LiteralString("object_capability"),
		typ.LiteralString("deny"),
		typ.LiteralString("denied"),
		typ.LiteralString("none"),
		typ.LiteralString("explicit"),
		typ.LiteralString("grant"),
		typ.LiteralString("grants"),
		typ.LiteralString("policy"),
	)
	linkModeType = typ.NewUnion(
		typ.LiteralString("required"),
		typ.LiteralString("detached"),
		typ.LiteralString("monitor"),
		typ.LiteralString("monitor_only"),
		typ.LiteralString("monitor-only"),
	)
	commandType = typ.NewUnion(typ.String, typ.Integer)

	limitsType = addLimitFields(typ.NewRecord()).Build()

	policyOptionsType  = addPolicyFields(typ.NewRecord()).Build()
	compileOptionsType = addPolicyFields(typ.NewRecord().
				OptField(evalOptionMethod, typ.String).
				OptField(evalOptionPolicy, policyOptionsType)).
				Build()
	spawnOptionsType = addPolicyFields(typ.NewRecord().
				OptField(evalOptionMethod, typ.String).
				OptField(evalOptionName, typ.String).
				OptField(evalOptionNetwork, typ.String).
				OptField(evalOptionInput, typ.Any).
				OptField(evalOptionContents, typ.NewArray(typ.Any)).
				OptField(evalOptionLink, linkModeType).
				OptField(evalOptionDetached, typ.Boolean).
				OptField(evalOptionMonitorOnly, typ.Boolean).
				OptField(evalOptionPolicy, policyOptionsType)).
				Build()

	programType = typ.NewInterface("eval.Program", []typ.Method{
		{Name: "spawn", Type: typ.Func().
			Param("self", typ.Self).
			OptParam("options", spawnOptionsType).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "evict", Type: typ.Func().
			Param("self", typ.Self).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
	})
)

func addLimitFields(b *typ.RecordBuilder) *typ.RecordBuilder {
	for _, spec := range limitSpecs {
		b = b.OptField(spec.name, typ.Integer)
	}
	return b
}

func addPolicyFields(b *typ.RecordBuilder) *typ.RecordBuilder {
	b = b.OptField(evalOptionCompile, compileModeType).
		OptField(evalOptionModules, typ.NewArray(typ.String)).
		OptField(evalOptionImports, typ.NewMap(typ.String, typ.String)).
		OptField(evalOptionBindings, typ.NewMap(typ.String, typ.Any)).
		OptField(evalOptionAllowClasses, typ.NewArray(typ.String)).
		OptField(evalOptionCommands, typ.NewArray(commandType)).
		OptField(evalOptionAllowCommands, typ.NewArray(commandType)).
		OptField(evalOptionSend, sendModeType).
		OptField(evalOptionSendTargets, typ.NewArray(typ.String))
	b = addLimitFields(b)
	return b.OptField(evalOptionLimits, limitsType).
		OptField(evalOptionAllowDetached, typ.Boolean)
}

// ModuleTypes declares the eval surface to the type checker.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest(Name)
	m.DefineType("Program", programType)
	m.DefineType("Limits", limitsType)
	m.DefineType("PolicyOptions", policyOptionsType)
	m.DefineType("CompileOptions", compileOptionsType)
	m.DefineType("SpawnOptions", spawnOptionsType)
	m.SetExport(typ.NewInterface(Name, []typ.Method{
		{Name: "compile", Type: typ.Func().
			Param("source", typ.String).
			OptParam("options", compileOptionsType).
			Returns(programType, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "spawn", Type: typ.Func().
			Param("source_or_program", typ.NewUnion(typ.String, programType)).
			OptParam("options", spawnOptionsType).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "evict", Type: typ.Func().
			Param("program", programType).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
	}))
	return m
}
