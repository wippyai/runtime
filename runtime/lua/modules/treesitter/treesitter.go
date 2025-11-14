package treesitter

import (
	"fmt"

	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Module is the Lua module for the Tree-sitter bindings.
type Module struct {
	languages *Languages
	log       *zap.Logger
}

// NewTreeSitterModule creates a new Tree-sitter module.
func NewTreeSitterModule(log *zap.Logger) *Module {
	return &Module{
		languages: NewLanguages(),
		log:       log,
	}
}

// Name is the module name.
func (m *Module) Name() string {
	return "treesitter"
}

// Loader is the module loader function.
func (m *Module) Loader(l *lua.LState) int {
	t := l.CreateTable(0, 5) // 5 function entries

	registerParser(l)
	registerTree(l)
	registerNode(l)
	registerQuery(l)
	registerCursor(l)
	registerLanguage(l)

	// Add module functions directly for better performance
	t.RawSetString("supported_languages", l.NewFunction(m.supportedLanguages))
	t.RawSetString("language", l.NewFunction(m.language))
	t.RawSetString("parser", l.NewFunction(newParser))
	t.RawSetString("parse", l.NewFunction(m.parse))
	t.RawSetString("query", l.NewFunction(newQuery))

	l.Push(t)
	return 1
}

// supportedLanguages returns a table of supported languages.
func (m *Module) supportedLanguages(l *lua.LState) int {
	langs := m.languages.GetSupportedLanguages()
	table := l.NewTable()
	for _, lang := range langs {
		table.RawSetString(lang, lua.LTrue)
	}
	l.Push(table)
	return 1
}

func (m *Module) language(l *lua.LState) int {
	languageAlias := l.CheckString(1)

	langInfo := m.languages.GetLanguageInfo(languageAlias)
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
func (m *Module) parse(l *lua.LState) int {
	if l.GetTop() != 2 {
		l.ArgError(1, "expected 2 arguments: language, code")
		return 0
	}

	languageAlias := l.CheckString(1)
	code := l.CheckString(2)

	// Spawn parser and set language
	parser := treesitter.NewParser()
	defer parser.Close()

	langInfo := m.languages.GetLanguageInfo(languageAlias)
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

	// Use context from Lua state if available
	ctx := l.Context()

	uw := engine.GetUnitOfWork(ctx)
	if uw == nil {
		l.RaiseError("unit of work is not found")
		return 0
	}

	var cflag uintptr
	parser.SetCancellationFlag(&cflag)

	// Parse with context
	tree := parser.ParseCtx(uw.Context(), []byte(code), nil)
	if tree == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("failed to parse code"))
		return 2
	}

	// Use the new constructor
	treeWrapper := NewTree(uw, tree, code)

	// Return tree userdata
	ud := l.NewUserData()
	ud.Value = treeWrapper
	ud.Metatable = value.GetTypeMetatable(l, "treesitter.Tree")

	l.Push(ud)
	return 1
}
