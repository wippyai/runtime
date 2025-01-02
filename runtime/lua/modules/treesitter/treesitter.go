package treesitter

import (
	"context"
	"fmt"

	"github.com/ponyruntime/go-lua"
	treesitter "github.com/tree-sitter/go-tree-sitter"
	"go.uber.org/zap"
)

type Module struct {
	log *zap.Logger
}

func NewTreeSitterModule(log *zap.Logger) *Module {
	return &Module{
		log: log,
	}
}

// Name is the module name.
func (m *Module) Name() string {
	return "treesitter"
}

// Loader is the module loader function.
func (m *Module) Loader(l *lua.LState) int {
	// Register our types first
	registerTree(l)
	registerNode(l)
	registerCursor(l)

	// Create the module table
	t := l.NewTable()

	lapi := map[string]lua.LGFunction{
		"supported_languages": m.supportedLanguages,
		"parse":               m.parse,
		//"query":               m.query, // in separate file
	}

	l.SetFuncs(t, lapi)
	l.Push(t)
	return 1
}

// supportedLanguages returns a table of supported languages.
func (m *Module) supportedLanguages(l *lua.LState) int {
	langs := GetSupportedLanguages()
	table := l.NewTable()
	for _, lang := range langs {
		table.RawSetString(lang, lua.LTrue)
	}
	l.Push(table)
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

	langInfo := GetLanguageInfo(languageAlias)
	if langInfo == nil {
		l.ArgError(1, fmt.Sprintf("unsupported language: %s", languageAlias))
		return 0
	}

	if langInfo.Language == nil {
		l.ArgError(1, fmt.Sprintf("language '%s' does not have a Tree-sitter language binding", languageAlias))
		return 0
	}

	if code == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("code is empty"))
		return 2
	}

	parser := treesitter.NewParser()
	defer parser.Close()

	langFunc := langInfo.Language
	if langFunc == nil {
		l.ArgError(1, fmt.Sprintf("language function for '%s' is not defined", languageAlias))
		return 0
	}

	lang := langFunc()
	err := parser.SetLanguage(treesitter.NewLanguage(lang))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to set language: %s", err)))
		return 2
	}

	ctx := l.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	tr := parser.ParseCtx(ctx, []byte(code), nil)
	if tr == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("failed to parse code"))
		return 2
	}

	// Create and return Tree userdata
	ud := l.NewUserData()
	ud.Value = &TreeWrapper{tree: tr}
	l.SetMetatable(ud, l.GetTypeMetatable("treesitter.Tree"))
	l.Push(ud)

	return 1
}
