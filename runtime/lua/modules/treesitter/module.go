package treesitter

import (
	"fmt"
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	lua "github.com/yuin/gopher-lua"
)

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

var (
	moduleTable  *lua.LTable
	registration *luaapi.Registration
	initOnce     sync.Once
	languages    *Languages
)

// Module is the singleton treesitter module instance.
var Module = &treesitterModule{}

type treesitterModule struct{}

func (m *treesitterModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "treesitter",
		Description: "Tree-sitter parsing and syntax analysis",
		Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
	}
}

func (m *treesitterModule) Register(l *lua.LState) *luaapi.Registration {
	initOnce.Do(func() {
		languages = NewLanguages()

		registerParser(l)
		registerTree(l)
		registerNode(l)
		registerQuery(l)
		registerCursor(l)
		registerLanguage(l)

		mod := lua.CreateTable(0, 5)
		mod.RawSetString("supported_languages", lua.LGoFunc(supportedLanguages))
		mod.RawSetString("language", lua.LGoFunc(language))
		mod.RawSetString("parser", lua.LGoFunc(newParser))
		mod.RawSetString("parse", lua.LGoFunc(parse))
		mod.RawSetString("query", lua.LGoFunc(newQuery))
		mod.Immutable = true
		moduleTable = mod

		registration = &luaapi.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})
	return registration
}

func (m *treesitterModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use luaapi.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	luaapi.LoadModule(l, Module)
}

// supportedLanguages returns a table of supported languages.
func supportedLanguages(l *lua.LState) int {
	langs := languages.GetSupportedLanguages()
	table := l.NewTable()
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
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("unsupported language: %s", languageAlias)))
		return 2
	}

	if langInfo.Language == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("language '%s' does not have a Tree-sitter language binding", languageAlias)))
		return 2
	}

	lang := treesitter.NewLanguage(langInfo.Language())

	// Spawn and return Language userdata
	ud := l.NewUserData()
	ud.Value = &LanguageWrapper{lang: lang}
	l.SetMetatable(ud, value.GetTypeMetatable(nil, "treesitter.Language"))
	l.Push(ud)
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

	// Spawn parser and set language
	parser := treesitter.NewParser()
	defer parser.Close()

	langInfo := languages.GetLanguageInfo(languageAlias)
	if langInfo == nil {
		l.ArgError(1, fmt.Sprintf("unsupported language: %s", languageAlias))
		return 0
	}

	if langInfo.Language == nil {
		l.ArgError(1, fmt.Sprintf("language '%s' does not have a Tree-sitter language binding", languageAlias))
		return 0
	}

	lang := langInfo.Language()
	err := parser.SetLanguage(treesitter.NewLanguage(lang))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to set language: %s", err)))
		return 2
	}

	// Use context from Lua state
	ctx := l.Context()
	if ctx == nil {
		l.RaiseError("no context found")
		return 0
	}

	var cflag uintptr
	parser.SetCancellationFlag(&cflag)

	// Parse with context
	tree := parser.ParseCtx(ctx, []byte(code), nil)
	if tree == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("failed to parse code"))
		return 2
	}

	// Use the new constructor
	treeWrapper := NewTree(ctx, tree, code)

	// Return tree userdata
	ud := l.NewUserData()
	ud.Value = treeWrapper
	ud.Metatable = value.GetTypeMetatable(l, "treesitter.Tree")

	l.Push(ud)
	return 1
}
