package text

import (
	"github.com/yuin/gopher-lua/types"
)

// Regexp userdata type
var regexpType = &types.InterfaceType{
	Name: "text.Regexp",
	Methods: map[string]*types.FunctionType{
		"find_all_string_submatch": types.NewFunction([]types.Type{types.String}, []types.Type{types.Any}),
		"find_string_submatch":     types.NewFunction([]types.Type{types.String}, []types.Type{types.Any}),
		"find_all_string":          types.NewFunction([]types.Type{types.String}, []types.Type{types.Any}),
		"find_string":              types.NewFunction([]types.Type{types.String}, []types.Type{types.Optional(types.String)}),
		"find_all_string_index":    types.NewFunction([]types.Type{types.String}, []types.Type{types.Any}),
		"find_string_index":        types.NewFunction([]types.Type{types.String}, []types.Type{types.Any}),
		"replace_all_string":       types.NewFunction([]types.Type{types.String, types.String}, []types.Type{types.String}),
		"match_string":             types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean}),
		"split":                    types.NewFunction([]types.Type{types.String, types.Optional(types.Number)}, []types.Type{types.Any}),
		"num_subexp":               types.NewFunction(nil, []types.Type{types.Number}),
		"subexp_names":             types.NewFunction(nil, []types.Type{types.Any}),
		"string":                   types.NewFunction(nil, []types.Type{types.String}),
	},
}

// DiffResult type
var diffResultType = &types.RecordType{
	Name: "text.DiffResult",
	Fields: []types.RecordField{
		{Name: "operation", Type: types.String},
		{Name: "text", Type: types.String},
	},
}

// Differ userdata type
var differType = &types.InterfaceType{
	Name: "text.Differ",
	Methods: map[string]*types.FunctionType{
		"compare":     types.NewFunction([]types.Type{types.String, types.String}, []types.Type{types.NewArray(diffResultType, false), types.Optional(types.LuaError)}),
		"pretty_text": types.NewFunction([]types.Type{types.Any}, []types.Type{types.String, types.Optional(types.LuaError)}),
		"pretty_html": types.NewFunction([]types.Type{types.Any}, []types.Type{types.String, types.Optional(types.LuaError)}),
		"patch_make":  types.NewFunction([]types.Type{types.String, types.String}, []types.Type{types.Any, types.Optional(types.LuaError)}),
		"patch_apply": types.NewFunction([]types.Type{types.Any, types.String}, []types.Type{types.String, types.Boolean}),
		"summarize":   types.NewFunction([]types.Type{types.Any}, []types.Type{types.Any}),
	},
}

// Splitter userdata type
var splitterType = &types.InterfaceType{
	Name: "text.Splitter",
	Methods: map[string]*types.FunctionType{
		"split_text":  types.NewFunction([]types.Type{types.String}, []types.Type{types.NewArray(types.String, false), types.Optional(types.LuaError)}),
		"split_batch": types.NewFunction([]types.Type{types.Any}, []types.Type{types.Any, types.Optional(types.LuaError)}),
	},
}

// Submodule types
var regexpModType = &types.InterfaceType{
	Name: "text.regexp",
	Methods: map[string]*types.FunctionType{
		// regexp.compile(pattern: string): Regexp, Error?
		"compile": types.NewFunction([]types.Type{types.String}, []types.Type{regexpType, types.Optional(types.LuaError)}),
	},
}

var diffModType = &types.InterfaceType{
	Name: "text.diff",
	Methods: map[string]*types.FunctionType{
		// diff.new(options?: table): Differ, Error?
		"new": types.NewFunction([]types.Type{types.Optional(types.Any)}, []types.Type{differType, types.Optional(types.LuaError)}),
	},
}

var splitterModType = &types.InterfaceType{
	Name: "text.splitter",
	Methods: map[string]*types.FunctionType{
		// splitter.recursive(options?: table): Splitter, Error?
		"recursive": types.NewFunction([]types.Type{types.Optional(types.Any)}, []types.Type{splitterType, types.Optional(types.LuaError)}),
		// splitter.markdown(options?: table): Splitter, Error?
		"markdown": types.NewFunction([]types.Type{types.Optional(types.Any)}, []types.Type{splitterType, types.Optional(types.LuaError)}),
	},
}

// ModuleTypes returns the type manifest for the text module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("text")

	m.DefineType("Regexp", regexpType)
	m.DefineType("Differ", differType)
	m.DefineType("Splitter", splitterType)
	m.DefineType("DiffResult", diffResultType)

	moduleType := &types.InterfaceType{
		Name: "text",
		Fields: map[string]types.Type{
			"regexp":   regexpModType,
			"diff":     diffModType,
			"splitter": splitterModType,
		},
	}

	m.SetExport(moduleType)
	return m
}
