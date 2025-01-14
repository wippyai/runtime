package treesitter

import "C"
import (
	treesitter "github.com/tree-sitter/go-tree-sitter"
	lua "github.com/yuin/gopher-lua"
)

// LanguageWrapper wraps a tree-sitter Language for Lua integration
type LanguageWrapper struct {
	lang *treesitter.Language
}

// Register the Language type to Lua
func registerLanguage(L *lua.LState) {
	mt := L.NewTypeMetatable("treesitter.Language")
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), languageMethods))
}

var languageMethods = map[string]lua.LGFunction{
	"version":            languageVersion,
	"node_kind_count":    languageNodeKindCount,
	"parse_state_count":  languageParseStateCount,
	"node_kind_for_id":   languageNodeKindForId,
	"id_for_node_kind":   languageIdForNodeKind,
	"node_kind_is_named": languageNodeKindIsNamed,
	"field_count":        languageFieldCount,
	"field_name_for_id":  languageFieldNameForId,
	"field_id_for_name":  languageFieldIdForName,
}

func checkLanguage(L *lua.LState) *LanguageWrapper {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*LanguageWrapper); ok {
		return v
	}
	L.ArgError(1, "Language expected")
	return nil
}

// Language methods implementation
func languageVersion(L *lua.LState) int {
	lang := checkLanguage(L)
	if lang.lang == nil {
		L.Push(lua.LNil)
		return 1
	}
	L.Push(lua.LNumber(lang.lang.Version()))
	return 1
}

func languageNodeKindCount(L *lua.LState) int {
	lang := checkLanguage(L)
	if lang.lang == nil {
		L.Push(lua.LNil)
		return 1
	}
	L.Push(lua.LNumber(lang.lang.NodeKindCount()))
	return 1
}

func languageParseStateCount(L *lua.LState) int {
	lang := checkLanguage(L)
	if lang.lang == nil {
		L.Push(lua.LNil)
		return 1
	}
	L.Push(lua.LNumber(lang.lang.ParseStateCount()))
	return 1
}

func languageNodeKindForId(L *lua.LState) int {
	lang := checkLanguage(L)
	if lang.lang == nil {
		L.Push(lua.LNil)
		return 1
	}
	id := uint16(L.CheckNumber(2))
	L.Push(lua.LString(lang.lang.NodeKindForId(id)))
	return 1
}

func languageIdForNodeKind(L *lua.LState) int {
	lang := checkLanguage(L)
	if lang.lang == nil {
		L.Push(lua.LNil)
		return 1
	}
	kind := L.CheckString(2)
	named := L.CheckBool(3)
	L.Push(lua.LNumber(lang.lang.IdForNodeKind(kind, named)))
	return 1
}

func languageNodeKindIsNamed(L *lua.LState) int {
	lang := checkLanguage(L)
	if lang.lang == nil {
		L.Push(lua.LNil)
		return 1
	}
	id := uint16(L.CheckNumber(2))
	L.Push(lua.LBool(lang.lang.NodeKindIsNamed(id)))
	return 1
}

func languageFieldCount(L *lua.LState) int {
	lang := checkLanguage(L)
	if lang.lang == nil {
		L.Push(lua.LNil)
		return 1
	}
	L.Push(lua.LNumber(lang.lang.FieldCount()))
	return 1
}

func languageFieldNameForId(L *lua.LState) int {
	lang := checkLanguage(L)
	if lang.lang == nil {
		L.Push(lua.LNil)
		return 1
	}
	id := uint16(L.CheckNumber(2))
	L.Push(lua.LString(lang.lang.FieldNameForId(id)))
	return 1
}

func languageFieldIdForName(L *lua.LState) int {
	lang := checkLanguage(L)
	if lang.lang == nil {
		L.Push(lua.LNil)
		return 1
	}
	name := L.CheckString(2)
	L.Push(lua.LNumber(lang.lang.FieldIdForName(name)))
	return 1
}
