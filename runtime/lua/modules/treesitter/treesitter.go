package treesitter

import (
	"context"
	"fmt"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// todo: a good chunk of memory optimizations is needed here, but no rush
type Module struct {
	languages *Languages
	log       *zap.Logger
}

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
	t := l.NewTable()

	registerParser(l)
	registerTree(l)
	registerNode(l)
	registerQuery(l)
	registerCursor(l)
	registerLanguage(l)

	lapi := map[string]lua.LGFunction{
		"supported_languages": m.supportedLanguages,
		"language":            m.language,
		"parser":              newParser,
		"parse":               m.parse,
		"query":               newQuery,
	}

	l.SetFuncs(t, lapi)
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

	// Create and return Language userdata
	ud := l.NewUserData()
	ud.Value = &LanguageWrapper{lang: lang}
	l.SetMetatable(ud, l.GetTypeMetatable("treesitter.Language"))
	l.Push(ud)
	return 1
}

// parse parses the text into a Tree object.
func (m *Module) parse(l *lua.LState) int {
	m.log.Debug("parse called")

	if l.GetTop() != 2 {
		l.ArgError(1, "expected 2 arguments: language, code")
		return 0
	}

	languageAlias := l.CheckString(1)
	code := l.CheckString(2)

	// Create parser and set language
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
	if ctx == nil {
		ctx = context.Background()
	}

	// Parse with context
	tree := parser.ParseCtx(ctx, []byte(code), nil)
	if tree == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("failed to parse code"))
		return 2
	}

	// Return tree userdata
	ud := l.NewUserData()
	ud.Value = &TreeWrapper{tree: tree, source: code}
	l.SetMetatable(ud, l.GetTypeMetatable("treesitter.Tree"))
	l.Push(ud)

	return 1
}
