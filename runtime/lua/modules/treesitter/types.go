// SPDX-License-Identifier: MPL-2.0

//go:build treesitter

package treesitter

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// Point type
var pointType = typ.NewRecord().Field("row", typ.Number).Field("column", typ.Number).Build()

// Range type
var rangeType = typ.NewRecord().Field("start_byte", typ.Number).Field("end_byte", typ.Number).Field("start_point", pointType).Field("end_point", pointType).Build()

// Forward declarations for types that reference each other
var (
	parserType   typ.Type
	treeType     typ.Type
	nodeType     typ.Type
	queryType    typ.Type
	cursorType   typ.Type
	languageType typ.Type
)

func init() {
	// Language type
	languageType = typ.NewInterface("treesitter.Language", []typ.Method{
		{Name: "version", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "node_kind_count", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "parse_state_count", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "node_kind_for_id", Type: typ.Func().Param("self", typ.Self).Param("id", typ.Number).Returns(typ.String).Build()},
		{Name: "id_for_node_kind", Type: typ.Func().Param("self", typ.Self).Param("kind", typ.String).Param("named", typ.Boolean).Returns(typ.Number).Build()},
		{Name: "node_kind_is_named", Type: typ.Func().Param("self", typ.Self).Param("id", typ.Number).Returns(typ.Boolean).Build()},
		{Name: "field_count", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "field_name_for_id", Type: typ.Func().Param("self", typ.Self).Param("id", typ.Number).Returns(typ.String).Build()},
		{Name: "field_id_for_name", Type: typ.Func().Param("self", typ.Self).Param("name", typ.String).Returns(typ.Number).Build()},
	})

	// Node type (methods return typ.Self for self-referential returns)
	nodeType = typ.NewInterface("treesitter.Node", []typ.Method{
		{Name: "parent", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(typ.Self)).Build()},
		{Name: "child", Type: typ.Func().Param("self", typ.Self).Param("index", typ.Number).Returns(typ.NewOptional(typ.Self)).Build()},
		{Name: "child_count", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "next_sibling", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(typ.Self)).Build()},
		{Name: "prev_sibling", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(typ.Self)).Build()},
		{Name: "next_named_sibling", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(typ.Self)).Build()},
		{Name: "prev_named_sibling", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(typ.Self)).Build()},
		{Name: "named_child", Type: typ.Func().Param("self", typ.Self).Param("index", typ.Number).Returns(typ.NewOptional(typ.Self)).Build()},
		{Name: "named_child_count", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "named_descendant_for_point_range", Type: typ.Func().Param("self", typ.Self).Param("start", pointType).Param("end", pointType).Returns(typ.NewOptional(typ.Self)).Build()},
		{Name: "descendant_count", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "child_by_field_name", Type: typ.Func().Param("self", typ.Self).Param("name", typ.String).Returns(typ.NewOptional(typ.Self)).Build()},
		{Name: "field_name_for_child", Type: typ.Func().Param("self", typ.Self).Param("index", typ.Number).Returns(typ.String).Build()},
		{Name: "kind", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
		{Name: "type", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
		{Name: "is_named", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
		{Name: "grammar_name", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
		{Name: "is_extra", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
		{Name: "is_missing", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
		{Name: "has_error", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
		{Name: "is_error", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
		{Name: "start_byte", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "end_byte", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "start_point", Type: typ.Func().Param("self", typ.Self).Returns(pointType).Build()},
		{Name: "end_point", Type: typ.Func().Param("self", typ.Self).Returns(pointType).Build()},
		{Name: "text", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
		{Name: "to_sexp", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
	})

	// Cursor type needs nodeType, so it's defined after nodeType is set
	cursorType = typ.NewInterface("treesitter.Cursor", []typ.Method{
		{Name: "current_node", Type: typ.Func().Param("self", typ.Self).Returns(nodeType).Build()},
		{Name: "current_field_id", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "current_field_name", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
		{Name: "current_depth", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "current_descendant_index", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "goto_parent", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
		{Name: "goto_first_child", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
		{Name: "goto_last_child", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
		{Name: "goto_next_sibling", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
		{Name: "goto_previous_sibling", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
		{Name: "goto_descendant", Type: typ.Func().Param("self", typ.Self).Param("index", typ.Number).Build()},
		{Name: "goto_first_child_for_byte", Type: typ.Func().Param("self", typ.Self).Param("byte", typ.Number).Returns(typ.Boolean).Build()},
		{Name: "goto_first_child_for_point", Type: typ.Func().Param("self", typ.Self).Param("point", pointType).Returns(typ.Boolean).Build()},
		{Name: "reset", Type: typ.Func().Param("self", typ.Self).Param("node", nodeType).Build()},
		{Name: "reset_to", Type: typ.Func().Param("self", typ.Self).Param("cursor", typ.Self).Build()},
		{Name: "copy", Type: typ.Func().Param("self", typ.Self).Returns(typ.Self).Build()},
		{Name: "close", Type: typ.Func().Param("self", typ.Self).Build()},
	})

	// Tree type needs nodeType and cursorType
	treeType = typ.NewInterface("treesitter.Tree", []typ.Method{
		{Name: "root_node", Type: typ.Func().Param("self", typ.Self).Returns(nodeType).Build()},
		{Name: "root_node_with_offset", Type: typ.Func().Param("self", typ.Self).Param("offset_bytes", typ.Number).Param("extent", pointType).Returns(nodeType).Build()},
		{Name: "language", Type: typ.Func().Param("self", typ.Self).Returns(languageType).Build()},
		{Name: "copy", Type: typ.Func().Param("self", typ.Self).Returns(typ.Self).Build()},
		{Name: "walk", Type: typ.Func().Param("self", typ.Self).Returns(cursorType).Build()},
		{Name: "edit", Type: typ.Func().Param("self", typ.Self).Param("edit", typ.Any).Build()},
		{Name: "close", Type: typ.Func().Param("self", typ.Self).Build()},
		{Name: "changed_ranges", Type: typ.Func().Param("self", typ.Self).Param("other", typ.Self).Returns(typ.NewArray(rangeType)).Build()},
		{Name: "included_ranges", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewArray(rangeType)).Build()},
		{Name: "dot_graph", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
	})

	// Parser type needs treeType and languageType
	parserType = typ.NewInterface("treesitter.Parser", []typ.Method{
		{Name: "parse", Type: typ.Func().Param("self", typ.Self).Param("text", typ.String).OptParam("old_tree", treeType).Returns(treeType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "set_language", Type: typ.Func().Param("self", typ.Self).Param("language", typ.String).Returns(typ.NewOptional(typ.LuaError)).Build()},
		{Name: "get_language", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(languageType)).Build()},
		{Name: "reset", Type: typ.Func().Param("self", typ.Self).Build()},
		{Name: "close", Type: typ.Func().Param("self", typ.Self).Build()},
		{Name: "set_timeout", Type: typ.Func().Param("self", typ.Self).Param("timeout", typ.Any).Build()},
		{Name: "set_ranges", Type: typ.Func().Param("self", typ.Self).Param("ranges", typ.NewArray(rangeType)).Build()},
	})

	// Query type needs nodeType
	queryType = typ.NewInterface("treesitter.Query", []typ.Method{
		{Name: "close", Type: typ.Func().Param("self", typ.Self).Build()},
		{Name: "matches", Type: typ.Func().Param("self", typ.Self).Param("node", nodeType).Param("text", typ.String).Returns(typ.Any).Build()},
		{Name: "captures", Type: typ.Func().Param("self", typ.Self).Param("node", nodeType).Param("text", typ.String).Returns(typ.Any).Build()},
		{Name: "pattern_count", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "capture_count", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "string_count", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "start_byte_for_pattern", Type: typ.Func().Param("self", typ.Self).Param("pattern", typ.Number).Returns(typ.Number).Build()},
		{Name: "set_byte_range", Type: typ.Func().Param("self", typ.Self).Param("start", typ.Number).Param("end", typ.Number).Build()},
		{Name: "set_point_range", Type: typ.Func().Param("self", typ.Self).Param("start", pointType).Param("end", pointType).Build()},
		{Name: "set_match_limit", Type: typ.Func().Param("self", typ.Self).Param("limit", typ.Number).Build()},
		{Name: "get_match_limit", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "did_exceed_match_limit", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
		{Name: "set_timeout", Type: typ.Func().Param("self", typ.Self).Param("timeout", typ.Any).Build()},
		{Name: "get_timeout", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "disable_pattern", Type: typ.Func().Param("self", typ.Self).Param("pattern", typ.Number).Build()},
		{Name: "disable_capture", Type: typ.Func().Param("self", typ.Self).Param("capture", typ.String).Build()},
		{Name: "is_pattern_rooted", Type: typ.Func().Param("self", typ.Self).Param("pattern", typ.Number).Returns(typ.Boolean).Build()},
		{Name: "is_pattern_non_local", Type: typ.Func().Param("self", typ.Self).Param("pattern", typ.Number).Returns(typ.Boolean).Build()},
		{Name: "capture_name_for_id", Type: typ.Func().Param("self", typ.Self).Param("id", typ.Number).Returns(typ.String).Build()},
		{Name: "capture_quantifier", Type: typ.Func().Param("self", typ.Self).Param("pattern", typ.Number).Param("capture", typ.Number).Returns(typ.String).Build()},
		{Name: "set_max_start_depth", Type: typ.Func().Param("self", typ.Self).Param("depth", typ.Number).Build()},
		{Name: "get_property_predicates", Type: typ.Func().Param("self", typ.Self).Param("pattern", typ.Number).Returns(typ.Any).Build()},
		{Name: "get_property_settings", Type: typ.Func().Param("self", typ.Self).Param("pattern", typ.Number).Returns(typ.Any).Build()},
		{Name: "is_pattern_guaranteed", Type: typ.Func().Param("self", typ.Self).Param("pattern", typ.Number).Returns(typ.Boolean).Build()},
		{Name: "capture_index_for_name", Type: typ.Func().Param("self", typ.Self).Param("name", typ.String).Returns(typ.Number).Build()},
		{Name: "end_byte_for_pattern", Type: typ.Func().Param("self", typ.Self).Param("pattern", typ.Number).Returns(typ.Number).Build()},
		{Name: "get_text_predicates", Type: typ.Func().Param("self", typ.Self).Param("pattern", typ.Number).Returns(typ.Any).Build()},
	})
}

// ModuleTypes returns the type manifest for the treesitter module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("treesitter")

	m.DefineType("Parser", parserType)
	m.DefineType("Tree", treeType)
	m.DefineType("Node", nodeType)
	m.DefineType("Query", queryType)
	m.DefineType("Cursor", cursorType)
	m.DefineType("Language", languageType)
	m.DefineType("Point", pointType)
	m.DefineType("Range", rangeType)

	moduleType := typ.NewInterface("treesitter", []typ.Method{
		{Name: "supported_languages", Type: typ.Func().Returns(typ.NewMap(typ.String, typ.Boolean)).Build()},
		{Name: "language", Type: typ.Func().Param("name", typ.String).Returns(languageType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "parser", Type: typ.Func().Returns(parserType).Build()},
		{Name: "parse", Type: typ.Func().Param("language", typ.String).Param("text", typ.String).Returns(treeType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "query", Type: typ.Func().Param("language", typ.String).Param("query", typ.String).Returns(queryType, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
