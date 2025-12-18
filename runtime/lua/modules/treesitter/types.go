package treesitter

import "github.com/yuin/gopher-lua/types"

// Point type
var pointType = &types.RecordType{
	Name: "treesitter.Point",
	Fields: []types.RecordField{
		{Name: "row", Type: types.Number},
		{Name: "column", Type: types.Number},
	},
}

// Range type
var rangeType = &types.RecordType{
	Name: "treesitter.Range",
	Fields: []types.RecordField{
		{Name: "start_byte", Type: types.Number},
		{Name: "end_byte", Type: types.Number},
		{Name: "start_point", Type: pointType},
		{Name: "end_point", Type: pointType},
	},
}

// Forward declarations for types that reference each other
var (
	parserType   *types.InterfaceType
	treeType     *types.InterfaceType
	nodeType     *types.InterfaceType
	queryType    *types.InterfaceType
	cursorType   *types.InterfaceType
	languageType *types.InterfaceType
)

func init() {
	// Language type
	languageType = &types.InterfaceType{
		Name: "treesitter.Language",
		Methods: map[string]*types.FunctionType{
			"version":            types.NewFunction(nil, []types.Type{types.Number}),
			"node_kind_count":    types.NewFunction(nil, []types.Type{types.Number}),
			"parse_state_count":  types.NewFunction(nil, []types.Type{types.Number}),
			"node_kind_for_id":   types.NewFunction([]types.Type{types.Number}, []types.Type{types.String}),
			"id_for_node_kind":   types.NewFunction([]types.Type{types.String, types.Boolean}, []types.Type{types.Number}),
			"node_kind_is_named": types.NewFunction([]types.Type{types.Number}, []types.Type{types.Boolean}),
			"field_count":        types.NewFunction(nil, []types.Type{types.Number}),
			"field_name_for_id":  types.NewFunction([]types.Type{types.Number}, []types.Type{types.String}),
			"field_id_for_name":  types.NewFunction([]types.Type{types.String}, []types.Type{types.Number}),
		},
	}

	// Node type (self-referential)
	nodeType = &types.InterfaceType{
		Name:    "treesitter.Node",
		Methods: map[string]*types.FunctionType{},
	}
	nodeType.Methods["parent"] = types.NewFunction(nil, []types.Type{types.Optional(nodeType)})
	nodeType.Methods["child"] = types.NewFunction([]types.Type{types.Number}, []types.Type{types.Optional(nodeType)})
	nodeType.Methods["child_count"] = types.NewFunction(nil, []types.Type{types.Number})
	nodeType.Methods["next_sibling"] = types.NewFunction(nil, []types.Type{types.Optional(nodeType)})
	nodeType.Methods["prev_sibling"] = types.NewFunction(nil, []types.Type{types.Optional(nodeType)})
	nodeType.Methods["next_named_sibling"] = types.NewFunction(nil, []types.Type{types.Optional(nodeType)})
	nodeType.Methods["prev_named_sibling"] = types.NewFunction(nil, []types.Type{types.Optional(nodeType)})
	nodeType.Methods["named_child"] = types.NewFunction([]types.Type{types.Number}, []types.Type{types.Optional(nodeType)})
	nodeType.Methods["named_child_count"] = types.NewFunction(nil, []types.Type{types.Number})
	nodeType.Methods["named_descendant_for_point_range"] = types.NewFunction([]types.Type{pointType, pointType}, []types.Type{types.Optional(nodeType)})
	nodeType.Methods["descendant_count"] = types.NewFunction(nil, []types.Type{types.Number})
	nodeType.Methods["child_by_field_name"] = types.NewFunction([]types.Type{types.String}, []types.Type{types.Optional(nodeType)})
	nodeType.Methods["field_name_for_child"] = types.NewFunction([]types.Type{types.Number}, []types.Type{types.String})
	nodeType.Methods["kind"] = types.NewFunction(nil, []types.Type{types.String})
	nodeType.Methods["type"] = types.NewFunction(nil, []types.Type{types.String})
	nodeType.Methods["is_named"] = types.NewFunction(nil, []types.Type{types.Boolean})
	nodeType.Methods["grammar_name"] = types.NewFunction(nil, []types.Type{types.String})
	nodeType.Methods["is_extra"] = types.NewFunction(nil, []types.Type{types.Boolean})
	nodeType.Methods["is_missing"] = types.NewFunction(nil, []types.Type{types.Boolean})
	nodeType.Methods["has_error"] = types.NewFunction(nil, []types.Type{types.Boolean})
	nodeType.Methods["is_error"] = types.NewFunction(nil, []types.Type{types.Boolean})
	nodeType.Methods["start_byte"] = types.NewFunction(nil, []types.Type{types.Number})
	nodeType.Methods["end_byte"] = types.NewFunction(nil, []types.Type{types.Number})
	nodeType.Methods["start_point"] = types.NewFunction(nil, []types.Type{pointType})
	nodeType.Methods["end_point"] = types.NewFunction(nil, []types.Type{pointType})
	nodeType.Methods["text"] = types.NewFunction(nil, []types.Type{types.String})
	nodeType.Methods["to_sexp"] = types.NewFunction(nil, []types.Type{types.String})

	// Cursor type (self-referential)
	cursorType = &types.InterfaceType{
		Name:    "treesitter.Cursor",
		Methods: map[string]*types.FunctionType{},
	}
	cursorType.Methods["current_node"] = types.NewFunction(nil, []types.Type{nodeType})
	cursorType.Methods["current_field_id"] = types.NewFunction(nil, []types.Type{types.Number})
	cursorType.Methods["current_field_name"] = types.NewFunction(nil, []types.Type{types.String})
	cursorType.Methods["current_depth"] = types.NewFunction(nil, []types.Type{types.Number})
	cursorType.Methods["current_descendant_index"] = types.NewFunction(nil, []types.Type{types.Number})
	cursorType.Methods["goto_parent"] = types.NewFunction(nil, []types.Type{types.Boolean})
	cursorType.Methods["goto_first_child"] = types.NewFunction(nil, []types.Type{types.Boolean})
	cursorType.Methods["goto_last_child"] = types.NewFunction(nil, []types.Type{types.Boolean})
	cursorType.Methods["goto_next_sibling"] = types.NewFunction(nil, []types.Type{types.Boolean})
	cursorType.Methods["goto_previous_sibling"] = types.NewFunction(nil, []types.Type{types.Boolean})
	cursorType.Methods["goto_descendant"] = types.NewFunction([]types.Type{types.Number}, nil)
	cursorType.Methods["goto_first_child_for_byte"] = types.NewFunction([]types.Type{types.Number}, []types.Type{types.Boolean})
	cursorType.Methods["goto_first_child_for_point"] = types.NewFunction([]types.Type{pointType}, []types.Type{types.Boolean})
	cursorType.Methods["reset"] = types.NewFunction([]types.Type{nodeType}, nil)
	cursorType.Methods["reset_to"] = types.NewFunction([]types.Type{cursorType}, nil)
	cursorType.Methods["copy"] = types.NewFunction(nil, []types.Type{cursorType})
	cursorType.Methods["close"] = types.NewFunction(nil, nil)

	// Tree type
	treeType = &types.InterfaceType{
		Name:    "treesitter.Tree",
		Methods: map[string]*types.FunctionType{},
	}
	treeType.Methods["root_node"] = types.NewFunction(nil, []types.Type{nodeType})
	treeType.Methods["root_node_with_offset"] = types.NewFunction([]types.Type{types.Number, pointType}, []types.Type{nodeType})
	treeType.Methods["language"] = types.NewFunction(nil, []types.Type{languageType})
	treeType.Methods["copy"] = types.NewFunction(nil, []types.Type{treeType})
	treeType.Methods["walk"] = types.NewFunction(nil, []types.Type{cursorType})
	treeType.Methods["edit"] = types.NewFunction([]types.Type{types.Any}, nil)
	treeType.Methods["close"] = types.NewFunction(nil, nil)
	treeType.Methods["changed_ranges"] = types.NewFunction([]types.Type{treeType}, []types.Type{types.NewArray(rangeType, false)})
	treeType.Methods["included_ranges"] = types.NewFunction(nil, []types.Type{types.NewArray(rangeType, false)})
	treeType.Methods["dot_graph"] = types.NewFunction(nil, []types.Type{types.String})

	// Parser type
	parserType = &types.InterfaceType{
		Name: "treesitter.Parser",
		Methods: map[string]*types.FunctionType{
			"parse":        types.NewFunction([]types.Type{types.String, types.Optional(treeType)}, []types.Type{treeType, types.Optional(types.LuaError)}),
			"set_language": types.NewFunction([]types.Type{languageType}, []types.Type{types.Optional(types.LuaError)}),
			"get_language": types.NewFunction(nil, []types.Type{types.Optional(languageType)}),
			"reset":        types.NewFunction(nil, nil),
			"close":        types.NewFunction(nil, nil),
			"set_timeout":  types.NewFunction([]types.Type{types.Number}, nil),
			"set_ranges":   types.NewFunction([]types.Type{types.NewArray(rangeType, false)}, nil),
		},
	}

	// Query type
	queryType = &types.InterfaceType{
		Name: "treesitter.Query",
		Methods: map[string]*types.FunctionType{
			"close":                   types.NewFunction(nil, nil),
			"matches":                 types.NewFunction([]types.Type{nodeType}, []types.Type{types.Any}),
			"captures":                types.NewFunction([]types.Type{nodeType}, []types.Type{types.Any}),
			"pattern_count":           types.NewFunction(nil, []types.Type{types.Number}),
			"capture_count":           types.NewFunction(nil, []types.Type{types.Number}),
			"string_count":            types.NewFunction(nil, []types.Type{types.Number}),
			"start_byte_for_pattern":  types.NewFunction([]types.Type{types.Number}, []types.Type{types.Number}),
			"set_byte_range":          types.NewFunction([]types.Type{types.Number, types.Number}, nil),
			"set_point_range":         types.NewFunction([]types.Type{pointType, pointType}, nil),
			"set_match_limit":         types.NewFunction([]types.Type{types.Number}, nil),
			"get_match_limit":         types.NewFunction(nil, []types.Type{types.Number}),
			"did_exceed_match_limit":  types.NewFunction(nil, []types.Type{types.Boolean}),
			"set_timeout":             types.NewFunction([]types.Type{types.Number}, nil),
			"get_timeout":             types.NewFunction(nil, []types.Type{types.Number}),
			"disable_pattern":         types.NewFunction([]types.Type{types.Number}, nil),
			"disable_capture":         types.NewFunction([]types.Type{types.String}, nil),
			"is_pattern_rooted":       types.NewFunction([]types.Type{types.Number}, []types.Type{types.Boolean}),
			"is_pattern_non_local":    types.NewFunction([]types.Type{types.Number}, []types.Type{types.Boolean}),
			"capture_name_for_id":     types.NewFunction([]types.Type{types.Number}, []types.Type{types.String}),
			"capture_quantifier":      types.NewFunction([]types.Type{types.Number, types.Number}, []types.Type{types.String}),
			"set_max_start_depth":     types.NewFunction([]types.Type{types.Number}, nil),
			"get_property_predicates": types.NewFunction([]types.Type{types.Number}, []types.Type{types.Any}),
			"get_property_settings":   types.NewFunction([]types.Type{types.Number}, []types.Type{types.Any}),
			"is_pattern_guaranteed":   types.NewFunction([]types.Type{types.Number}, []types.Type{types.Boolean}),
			"capture_index_for_name":  types.NewFunction([]types.Type{types.String}, []types.Type{types.Number}),
			"end_byte_for_pattern":    types.NewFunction([]types.Type{types.Number}, []types.Type{types.Number}),
			"get_text_predicates":     types.NewFunction([]types.Type{types.Number}, []types.Type{types.Any}),
		},
	}
}

// ModuleTypes returns the type manifest for the treesitter module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("treesitter")

	m.DefineType("Parser", parserType)
	m.DefineType("Tree", treeType)
	m.DefineType("Node", nodeType)
	m.DefineType("Query", queryType)
	m.DefineType("Cursor", cursorType)
	m.DefineType("Language", languageType)
	m.DefineType("Point", pointType)
	m.DefineType("Range", rangeType)

	moduleType := &types.InterfaceType{
		Name: "treesitter",
		Methods: map[string]*types.FunctionType{
			"supported_languages": types.NewFunction(nil, []types.Type{types.NewMap(types.String, types.Boolean, false)}),
			"language":            types.NewFunction([]types.Type{types.String}, []types.Type{languageType, types.Optional(types.LuaError)}),
			"parser":              types.NewFunction(nil, []types.Type{parserType}),
			"parse":               types.NewFunction([]types.Type{types.String, types.String}, []types.Type{treeType, types.Optional(types.LuaError)}),
			"query":               types.NewFunction([]types.Type{languageType, types.String}, []types.Type{queryType, types.Optional(types.LuaError)}),
		},
	}

	m.SetExport(moduleType)
	return m
}
