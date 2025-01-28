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
func registerLanguage(l *lua.LState) {
	mt := l.NewTypeMetatable("treesitter.Language")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"version":            languageVersion,
		"node_kind_count":    languageNodeKindCount,
		"parse_state_count":  languageParseStateCount,
		"node_kind_for_id":   languageNodeKindForID,
		"id_for_node_kind":   languageIDForNodeKind,
		"node_kind_is_named": languageNodeKindIsNamed,
		"field_count":        languageFieldCount,
		"field_name_for_id":  languageFieldNameForID,
		"field_id_for_name":  languageFieldIDForName,
	}))
}

func checkLanguage(l *lua.LState) *LanguageWrapper {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*LanguageWrapper); ok {
		return v
	}
	l.ArgError(1, "Language expected")
	return nil
}

// Language methods implementation
func languageVersion(l *lua.LState) int {
	lang := checkLanguage(l)
	if lang.lang == nil {
		l.Push(lua.LNil)
		return 1
	}
	l.Push(lua.LNumber(lang.lang.Version()))
	return 1
}

func languageNodeKindCount(l *lua.LState) int {
	lang := checkLanguage(l)
	if lang.lang == nil {
		l.Push(lua.LNil)
		return 1
	}
	l.Push(lua.LNumber(lang.lang.NodeKindCount()))
	return 1
}

func languageParseStateCount(l *lua.LState) int {
	lang := checkLanguage(l)
	if lang.lang == nil {
		l.Push(lua.LNil)
		return 1
	}
	l.Push(lua.LNumber(lang.lang.ParseStateCount()))
	return 1
}

func languageNodeKindForID(l *lua.LState) int {
	lang := checkLanguage(l)
	if lang.lang == nil {
		l.Push(lua.LNil)
		return 1
	}
	id := uint16(l.CheckNumber(2))
	l.Push(lua.LString(lang.lang.NodeKindForId(id)))
	return 1
}

func languageIDForNodeKind(l *lua.LState) int {
	lang := checkLanguage(l)
	if lang.lang == nil {
		l.Push(lua.LNil)
		return 1
	}
	kind := l.CheckString(2)
	named := l.CheckBool(3)
	l.Push(lua.LNumber(lang.lang.IdForNodeKind(kind, named)))
	return 1
}

func languageNodeKindIsNamed(l *lua.LState) int {
	lang := checkLanguage(l)
	if lang.lang == nil {
		l.Push(lua.LNil)
		return 1
	}
	id := uint16(l.CheckNumber(2))
	l.Push(lua.LBool(lang.lang.NodeKindIsNamed(id)))
	return 1
}

func languageFieldCount(l *lua.LState) int {
	lang := checkLanguage(l)
	if lang.lang == nil {
		l.Push(lua.LNil)
		return 1
	}
	l.Push(lua.LNumber(lang.lang.FieldCount()))
	return 1
}

func languageFieldNameForID(l *lua.LState) int {
	lang := checkLanguage(l)
	if lang.lang == nil {
		l.Push(lua.LNil)
		return 1
	}
	id := uint16(l.CheckNumber(2))
	l.Push(lua.LString(lang.lang.FieldNameForId(id)))
	return 1
}

func languageFieldIDForName(l *lua.LState) int {
	lang := checkLanguage(l)
	if lang.lang == nil {
		l.Push(lua.LNil)
		return 1
	}
	name := l.CheckString(2)
	l.Push(lua.LNumber(lang.lang.FieldIdForName(name)))
	return 1
}
