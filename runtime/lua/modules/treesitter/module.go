// SPDX-License-Identifier: MPL-2.0

//go:build treesitter

package treesitter

import (
	"fmt"
	"time"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	lua "github.com/wippyai/go-lua"
)

// parseDuration parses a duration from Lua argument.
// Accepts: number (nanoseconds) or string ("1s", "500ms", "2h30m").
func parseDuration(l *lua.LState, idx int) (time.Duration, error) {
	arg := l.Get(idx)
	switch v := arg.(type) {
	case lua.LNumber:
		return time.Duration(v), nil
	case lua.LInteger:
		return time.Duration(v), nil
	case lua.LString:
		return time.ParseDuration(string(v))
	default:
		return 0, fmt.Errorf("duration must be number or string, got %T", arg)
	}
}

// isNumber checks if a LValue is a number (LNumber or LInteger)
func isNumber(v lua.LValue) bool {
	switch v.(type) {
	case lua.LNumber, lua.LInteger:
		return true
	default:
		return false
	}
}

// toUint converts LValue to uint, handling both LNumber and LInteger
func toUint(v lua.LValue) uint {
	switch n := v.(type) {
	case lua.LNumber:
		return uint(n)
	case lua.LInteger:
		return uint(n)
	default:
		return 0
	}
}

var languages *Languages

// Module is the treesitter module definition.
var Module = &luaapi.ModuleDef{
	Name:        "treesitter",
	Description: "Tree-sitter parsing and syntax analysis",
	Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
	Build:       buildModule,
	Types:       ModuleTypes,
}

func init() {
	languages = NewLanguages()

	value.RegisterTypeMethods(nil, typeParser, nil, map[string]lua.LGoFunc{
		"parse":        parserParse,
		"set_language": parserSetLanguage,
		"get_language": parserGetLanguage,
		"reset":        parserReset,
		"close":        parserClose,
		"set_timeout":  parserSetTimeout,
		"set_ranges":   parserSetRanges,
	})

	value.RegisterTypeMethods(nil, typeTree, nil, map[string]lua.LGoFunc{
		"root_node":             treeRootNode,
		"root_node_with_offset": treeRootNodeWithOffset,
		"language":              treeLanguage,
		"copy":                  treeCopy,
		"walk":                  treeWalk,
		"edit":                  treeEdit,
		"close":                 treeClose,
		"changed_ranges":        treeChangedRanges,
		"included_ranges":       treeIncludedRanges,
		"dot_graph":             treePrintDotGraph,
	})

	value.RegisterTypeMethods(nil, typeNode, nil, map[string]lua.LGoFunc{
		"parent":                           nodeParent,
		"child":                            nodeChild,
		"child_count":                      nodeChildCount,
		"next_sibling":                     nodeNextSibling,
		"prev_sibling":                     nodePrevSibling,
		"next_named_sibling":               nodeNextNamedSibling,
		"prev_named_sibling":               nodePrevNamedSibling,
		"named_child":                      nodeNamedChild,
		"named_child_count":                nodeNamedChildCount,
		"named_descendant_for_point_range": nodeNamedDescendantForPointRange,
		"descendant_count":                 nodeDescendantCount,
		"child_by_field_name":              nodeChildByFieldName,
		"field_name_for_child":             nodeFieldNameForChild,
		"kind":                             nodeKind,
		"type":                             nodeKind,
		"is_named":                         nodeIsNamed,
		"grammar_name":                     nodeGrammarName,
		"is_extra":                         nodeIsExtra,
		"is_missing":                       nodeIsMissing,
		"has_error":                        nodeHasError,
		"is_error":                         nodeIsError,
		"start_byte":                       nodeStartByte,
		"end_byte":                         nodeEndByte,
		"start_point":                      nodeStartPoint,
		"end_point":                        nodeEndPoint,
		"text":                             nodeText,
		"to_sexp":                          nodeToSexp,
	})

	value.RegisterTypeMethods(nil, typeQuery, nil, map[string]lua.LGoFunc{
		"close":                   queryClose,
		"matches":                 queryMatches,
		"captures":                queryCaptures,
		"pattern_count":           queryPatternCount,
		"capture_count":           queryCaptureCount,
		"string_count":            queryStringCount,
		"start_byte_for_pattern":  queryStartByteForPattern,
		"set_byte_range":          querySetByteRange,
		"set_point_range":         querySetPointRange,
		"set_match_limit":         querySetMatchLimit,
		"get_match_limit":         queryGetMatchLimit,
		"did_exceed_match_limit":  queryDidExceedMatchLimit,
		"set_timeout":             querySetTimeout,
		"get_timeout":             queryGetTimeout,
		"disable_pattern":         queryDisablePattern,
		"disable_capture":         queryDisableCapture,
		"is_pattern_rooted":       queryIsPatternRooted,
		"is_pattern_non_local":    queryIsPatternNonLocal,
		"capture_name_for_id":     queryCaptureNameForID,
		"capture_quantifier":      queryCaptureQuantifier,
		"set_max_start_depth":     querySetMaxStartDepth,
		"get_property_predicates": queryGetPropertyPredicates,
		"get_property_settings":   queryGetPropertySettings,
		"is_pattern_guaranteed":   queryIsPatternGuaranteed,
		"capture_index_for_name":  queryCaptureIndexForName,
		"end_byte_for_pattern":    queryEndByteForPattern,
		"get_text_predicates":     queryGetTextPredicates,
	})

	value.RegisterTypeMethods(nil, typeCursor, nil, map[string]lua.LGoFunc{
		"current_node":               cursorCurrentNode,
		"current_field_id":           cursorCurrentFieldID,
		"current_field_name":         cursorCurrentFieldName,
		"current_depth":              cursorCurrentDepth,
		"current_descendant_index":   cursorCurrentDescendantIndex,
		"goto_parent":                cursorGotoParent,
		"goto_first_child":           cursorGotoFirstChild,
		"goto_last_child":            cursorGotoLastChild,
		"goto_next_sibling":          cursorGotoNextSibling,
		"goto_previous_sibling":      cursorGotoPreviousSibling,
		"goto_descendant":            cursorGotoDescendant,
		"goto_first_child_for_byte":  cursorGotoFirstChildForByte,
		"goto_first_child_for_point": cursorGotoFirstChildForPoint,
		"reset":                      cursorReset,
		"reset_to":                   cursorResetTo,
		"copy":                       cursorCopy,
		"close":                      cursorClose,
	})

	value.RegisterTypeMethods(nil, typeLanguage, nil, map[string]lua.LGoFunc{
		"version":            languageVersion,
		"node_kind_count":    languageNodeKindCount,
		"parse_state_count":  languageParseStateCount,
		"node_kind_for_id":   languageNodeKindForID,
		"id_for_node_kind":   languageIDForNodeKind,
		"node_kind_is_named": languageNodeKindIsNamed,
		"field_count":        languageFieldCount,
		"field_name_for_id":  languageFieldNameForID,
		"field_id_for_name":  languageFieldIDForName,
	})
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	mod := lua.CreateTable(0, 5)
	mod.RawSetString("supported_languages", lua.LGoFunc(supportedLanguages))
	mod.RawSetString("language", lua.LGoFunc(language))
	mod.RawSetString("parser", lua.LGoFunc(newParser))
	mod.RawSetString("parse", lua.LGoFunc(parse))
	mod.RawSetString("query", lua.LGoFunc(newQuery))
	mod.Immutable = true
	return mod, nil
}

// supportedLanguages returns a table of supported languages.
func supportedLanguages(l *lua.LState) int {
	langs := languages.GetSupportedLanguages()
	table := lua.CreateTable(len(langs), 0)
	for _, lang := range langs {
		table.RawSetString(lang, lua.LTrue)
	}
	l.Push(table)
	return 1
}

func language(l *lua.LState) int {
	languageAlias := l.CheckString(1)

	langInfo := languages.GetLanguageInfo(languageAlias)
	if langInfo == nil {
		err := lua.NewLuaError(l, fmt.Sprintf("unsupported language: %s", languageAlias)).
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	if langInfo.Language == nil {
		err := lua.NewLuaError(l, fmt.Sprintf("language '%s' does not have a Tree-sitter language binding", languageAlias)).
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	lang := treesitter.NewLanguage(langInfo.Language())
	value.PushTypedUserData(l, &LanguageWrapper{lang: lang}, typeLanguage)
	return 1
}

// parse parses the text into a Tree object.
func parse(l *lua.LState) int {
	if l.GetTop() != 2 {
		l.ArgError(1, "expected 2 arguments: language, code")
		return 0
	}

	languageAlias := l.CheckString(1)
	code := l.CheckString(2)

	parser := treesitter.NewParser()
	defer parser.Close()

	langInfo := languages.GetLanguageInfo(languageAlias)
	if langInfo == nil {
		err := lua.NewLuaError(l, fmt.Sprintf("unsupported language: %s", languageAlias)).
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	if langInfo.Language == nil {
		err := lua.NewLuaError(l, fmt.Sprintf("language '%s' does not have a Tree-sitter language binding", languageAlias)).
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	lang := langInfo.Language()
	if setErr := parser.SetLanguage(treesitter.NewLanguage(lang)); setErr != nil {
		err := lua.WrapErrorWithLua(l, setErr, "failed to set language").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context found").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	codeBytes := []byte(code)
	readCallback := func(offset int, _ treesitter.Point) []byte {
		if offset >= len(codeBytes) {
			return nil
		}
		return codeBytes[offset:]
	}

	opts := &treesitter.ParseOptions{
		ProgressCallback: func(_ treesitter.ParseState) bool {
			return ctx.Err() != nil
		},
	}

	tree := parser.ParseWithOptions(readCallback, nil, opts)
	if tree == nil {
		err := lua.NewLuaError(l, "failed to parse code").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	treeWrapper := NewTree(ctx, tree, code)
	value.PushTypedUserData(l, treeWrapper, typeTree)
	return 1
}
