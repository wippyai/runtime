package security

import (
	"github.com/yuin/gopher-lua/types"
)

// Forward declarations for mutually referential types
var (
	actorType      *types.InterfaceType
	scopeType      *types.InterfaceType
	policyType     *types.InterfaceType
	tokenStoreType *types.InterfaceType
)

func init() {
	// Actor type
	actorType = &types.InterfaceType{
		Name: "security.Actor",
		Methods: map[string]*types.FunctionType{
			"id":   types.NewFunction(nil, []types.Type{types.String}),
			"meta": types.NewFunction(nil, []types.Type{types.NewMap(types.String, types.Any, false)}),
		},
	}

	// Policy type
	policyType = &types.InterfaceType{
		Name: "security.Policy",
		Methods: map[string]*types.FunctionType{
			"id":       types.NewFunction(nil, []types.Type{types.String}),
			"evaluate": types.NewFunction([]types.Type{actorType, types.String, types.String, types.Optional(types.Any)}, []types.Type{types.String}),
		},
	}

	// Scope type (self-referential and references policyType)
	scopeType = &types.InterfaceType{
		Name:    "security.Scope",
		Methods: map[string]*types.FunctionType{},
	}
	scopeType.Methods["with"] = types.NewFunction([]types.Type{policyType}, []types.Type{scopeType})
	scopeType.Methods["without"] = types.NewFunction([]types.Type{types.Any}, []types.Type{scopeType})
	scopeType.Methods["evaluate"] = types.NewFunction([]types.Type{actorType, types.String, types.String, types.Optional(types.Any)}, []types.Type{types.String})
	scopeType.Methods["contains"] = types.NewFunction([]types.Type{types.Any}, []types.Type{types.Boolean})
	scopeType.Methods["policies"] = types.NewFunction(nil, []types.Type{types.NewArray(policyType, false)})

	// TokenStore type
	tokenStoreType = &types.InterfaceType{
		Name: "security.TokenStore",
		Methods: map[string]*types.FunctionType{
			"validate": types.NewFunction([]types.Type{types.String}, []types.Type{actorType, scopeType, types.Optional(types.LuaError)}),
			"create":   types.NewFunction([]types.Type{actorType, scopeType, types.Optional(types.Any)}, []types.Type{types.String, types.Optional(types.LuaError)}),
			"revoke":   types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
			"close":    types.NewFunction(nil, []types.Type{types.Boolean}),
		},
	}
}

// ModuleTypes returns the type manifest for the security module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("security")

	m.DefineType("Actor", actorType)
	m.DefineType("Scope", scopeType)
	m.DefineType("Policy", policyType)
	m.DefineType("TokenStore", tokenStoreType)

	moduleType := &types.InterfaceType{
		Name: "security",
		Methods: map[string]*types.FunctionType{
			"actor":       types.NewFunction(nil, []types.Type{types.Optional(actorType)}),
			"scope":       types.NewFunction(nil, []types.Type{types.Optional(scopeType)}),
			"can":         types.NewFunction([]types.Type{types.String, types.String, types.Optional(types.Any)}, []types.Type{types.Boolean}),
			"policy":      types.NewFunction([]types.Type{types.String}, []types.Type{policyType, types.Optional(types.LuaError)}),
			"named_scope": types.NewFunction([]types.Type{types.String}, []types.Type{scopeType, types.Optional(types.LuaError)}),
			"new_scope":   types.NewFunction([]types.Type{types.Optional(types.NewArray(policyType, false))}, []types.Type{scopeType}),
			"new_actor":   types.NewFunction([]types.Type{types.String, types.Optional(types.Any)}, []types.Type{actorType}),
			"token_store": types.NewFunction([]types.Type{types.String}, []types.Type{tokenStoreType, types.Optional(types.LuaError)}),
		},
	}

	m.SetExport(moduleType)
	return m
}
