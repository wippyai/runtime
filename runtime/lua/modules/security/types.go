package security

import (
	"github.com/yuin/gopher-lua/types/io"
	"github.com/yuin/gopher-lua/types/typ"
)

// Forward declarations for mutually referential types
var (
	actorType      typ.Type
	scopeType      typ.Type
	policyType     typ.Type
	tokenStoreType typ.Type
)

func init() {
	// Actor type
	actorType = typ.NewInterface("security.Actor", []typ.Method{
		{Name: "id", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
		{Name: "meta", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewMap(typ.String, typ.Any)).Build()},
	})

	// Policy type
	policyType = typ.NewInterface("security.Policy", []typ.Method{
		{Name: "id", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
		{Name: "evaluate", Type: typ.Func().Param("self", typ.Self).Param("actor", actorType).Param("resource", typ.String).Param("action", typ.String).OptParam("context", typ.Any).Returns(typ.String).Build()},
	})

	// Scope type (self-referential and references policyType)
	scopeType = typ.NewInterface("security.Scope", []typ.Method{
		{Name: "with", Type: typ.Func().Param("self", typ.Self).Param("policy", policyType).Returns(typ.Self).Build()},
		{Name: "without", Type: typ.Func().Param("self", typ.Self).Param("policy", typ.Any).Returns(typ.Self).Build()},
		{Name: "evaluate", Type: typ.Func().Param("self", typ.Self).Param("actor", actorType).Param("resource", typ.String).Param("action", typ.String).OptParam("context", typ.Any).Returns(typ.String).Build()},
		{Name: "contains", Type: typ.Func().Param("self", typ.Self).Param("policy", typ.Any).Returns(typ.Boolean).Build()},
		{Name: "policies", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewArray(policyType)).Build()},
	})

	// TokenStore type
	tokenStoreType = typ.NewInterface("security.TokenStore", []typ.Method{
		{Name: "validate", Type: typ.Func().Param("self", typ.Self).Param("token", typ.String).Returns(actorType, scopeType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "create", Type: typ.Func().Param("self", typ.Self).Param("actor", actorType).Param("scope", scopeType).OptParam("meta", typ.Any).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "revoke", Type: typ.Func().Param("self", typ.Self).Param("token", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "close", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
	})
}

// ModuleTypes returns the type manifest for the security module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("security")

	m.DefineType("Actor", actorType)
	m.DefineType("Scope", scopeType)
	m.DefineType("Policy", policyType)
	m.DefineType("TokenStore", tokenStoreType)

	moduleType := typ.NewInterface("security", []typ.Method{
		{Name: "actor", Type: typ.Func().Returns(typ.NewOptional(actorType)).Build()},
		{Name: "scope", Type: typ.Func().Returns(typ.NewOptional(scopeType)).Build()},
		{Name: "can", Type: typ.Func().Param("resource", typ.String).Param("action", typ.String).OptParam("context", typ.Any).Returns(typ.Boolean).Build()},
		{Name: "policy", Type: typ.Func().Param("name", typ.String).Returns(policyType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "named_scope", Type: typ.Func().Param("name", typ.String).Returns(scopeType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "new_scope", Type: typ.Func().OptParam("policies", typ.NewArray(policyType)).Returns(scopeType).Build()},
		{Name: "new_actor", Type: typ.Func().Param("id", typ.String).OptParam("meta", typ.Any).Returns(actorType).Build()},
		{Name: "token_store", Type: typ.Func().Param("name", typ.String).Returns(tokenStoreType, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
