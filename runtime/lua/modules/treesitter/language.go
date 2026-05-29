// SPDX-License-Identifier: MPL-2.0

//go:build treesitter

package treesitter

import (
	treesitter "github.com/tree-sitter/go-tree-sitter"
	lua "github.com/wippyai/go-lua"
)

const typeLanguage = "treesitter.Language"

// LanguageWrapper wraps a tree-sitter Language for Lua integration
type LanguageWrapper struct {
	lang *treesitter.Language
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
	if lang == nil {
		return 0
	}
	if lang.lang == nil {
		l.Push(lua.LNil)
		return 1
	}
	l.Push(lua.LNumber(lang.lang.AbiVersion()))
	return 1
}

func languageNodeKindCount(l *lua.LState) int {
	lang := checkLanguage(l)
	if lang == nil {
		return 0
	}
	if lang.lang == nil {
		l.Push(lua.LNil)
		return 1
	}
	l.Push(lua.LNumber(lang.lang.NodeKindCount()))
	return 1
}

func languageParseStateCount(l *lua.LState) int {
	lang := checkLanguage(l)
	if lang == nil {
		return 0
	}
	if lang.lang == nil {
		l.Push(lua.LNil)
		return 1
	}
	l.Push(lua.LNumber(lang.lang.ParseStateCount()))
	return 1
}

func languageNodeKindForID(l *lua.LState) int {
	lang := checkLanguage(l)
	if lang == nil {
		return 0
	}
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
	if lang == nil {
		return 0
	}
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
	if lang == nil {
		return 0
	}
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
	if lang == nil {
		return 0
	}
	if lang.lang == nil {
		l.Push(lua.LNil)
		return 1
	}
	l.Push(lua.LNumber(lang.lang.FieldCount()))
	return 1
}

func languageFieldNameForID(l *lua.LState) int {
	lang := checkLanguage(l)
	if lang == nil {
		return 0
	}
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
	if lang == nil {
		return 0
	}
	if lang.lang == nil {
		l.Push(lua.LNil)
		return 1
	}
	name := l.CheckString(2)
	l.Push(lua.LNumber(lang.lang.FieldIdForName(name)))
	return 1
}
