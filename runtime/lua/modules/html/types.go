package html

import (
	"github.com/yuin/gopher-lua/types"
)

// Forward declarations for mutually referential types
var (
	policyType      *types.InterfaceType
	attrBuilderType *types.InterfaceType
)

func init() {
	// Policy type
	policyType = &types.InterfaceType{
		Name:    "html.Policy",
		Methods: map[string]*types.FunctionType{},
	}

	// AttrBuilder type
	attrBuilderType = &types.InterfaceType{
		Name:    "html.AttrBuilder",
		Methods: map[string]*types.FunctionType{},
	}

	// Policy methods (some return Policy, some reference AttrBuilder)
	policyType.Methods["allow_elements"] = &types.FunctionType{Params: nil, Variadic: types.String, Returns: []types.Type{policyType}}
	policyType.Methods["allow_attrs"] = &types.FunctionType{Params: nil, Variadic: types.String, Returns: []types.Type{attrBuilderType}}
	policyType.Methods["allow_standard_urls"] = types.NewFunction(nil, []types.Type{policyType})
	policyType.Methods["require_parseable_urls"] = types.NewFunction([]types.Type{types.Boolean}, []types.Type{policyType})
	policyType.Methods["allow_relative_urls"] = types.NewFunction([]types.Type{types.Boolean}, []types.Type{policyType})
	policyType.Methods["allow_url_schemes"] = &types.FunctionType{Params: nil, Variadic: types.String, Returns: []types.Type{policyType}}
	policyType.Methods["require_nofollow_on_links"] = types.NewFunction([]types.Type{types.Boolean}, []types.Type{policyType})
	policyType.Methods["require_noreferrer_on_links"] = types.NewFunction([]types.Type{types.Boolean}, []types.Type{policyType})
	policyType.Methods["add_target_blank_to_fully_qualified_links"] = types.NewFunction([]types.Type{types.Boolean}, []types.Type{policyType})
	policyType.Methods["allow_data_uri_images"] = types.NewFunction(nil, []types.Type{policyType})
	policyType.Methods["allow_standard_attributes"] = types.NewFunction(nil, []types.Type{policyType})
	policyType.Methods["allow_images"] = types.NewFunction(nil, []types.Type{policyType})
	policyType.Methods["allow_lists"] = types.NewFunction(nil, []types.Type{policyType})
	policyType.Methods["allow_tables"] = types.NewFunction(nil, []types.Type{policyType})
	policyType.Methods["sanitize"] = types.NewFunction([]types.Type{types.String}, []types.Type{types.String})

	// AttrBuilder methods
	attrBuilderType.Methods["on_elements"] = &types.FunctionType{Params: nil, Variadic: types.String, Returns: []types.Type{policyType}}
	attrBuilderType.Methods["globally"] = types.NewFunction(nil, []types.Type{policyType})
	attrBuilderType.Methods["matching"] = types.NewFunction([]types.Type{types.String}, []types.Type{attrBuilderType, types.Optional(types.LuaError)})
}

// sanitize submodule type
var sanitizeType = &types.InterfaceType{
	Name: "html.sanitize",
	Methods: map[string]*types.FunctionType{
		"new_policy":    {Params: nil, Returns: []types.Type{policyType, types.Optional(types.LuaError)}},
		"ugc_policy":    {Params: nil, Returns: []types.Type{policyType, types.Optional(types.LuaError)}},
		"strict_policy": {Params: nil, Returns: []types.Type{policyType, types.Optional(types.LuaError)}},
	},
}

// ModuleTypes returns the type manifest for the html module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("html")

	m.DefineType("Policy", policyType)
	m.DefineType("AttrBuilder", attrBuilderType)

	moduleType := &types.InterfaceType{
		Name: "html",
		Fields: map[string]types.Type{
			"sanitize": sanitizeType,
		},
	}

	m.SetExport(moduleType)
	return m
}
