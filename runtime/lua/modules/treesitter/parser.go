package treesitter

import (
	treesitter "github.com/tree-sitter/go-tree-sitter"
	"github.com/yuin/gopher-lua"
	"log"
)

type ParserWrapper struct {
	parser *treesitter.Parser
}

func registerParser(L *lua.LState) {
	mt := L.NewTypeMetatable("treesitter.Parser")
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), parserMethods))
	L.SetField(mt, "__gc", L.NewFunction(parserGC))
}

var parserMethods = map[string]lua.LGFunction{
	"parse":        parserParse,
	"set_language": parserSetLanguage,
	"get_language": parserGetLanguage,
}

func newParser(L *lua.LState) int {
	parser := treesitter.NewParser()

	ud := L.NewUserData()
	ud.Value = &ParserWrapper{parser: parser}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Parser"))
	L.Push(ud)
	return 1
}

func parserSetLanguage(L *lua.LState) int {
	p := checkParser(L)
	langAlias := L.CheckString(2)

	langInfo := GetLanguageInfo(langAlias)
	if langInfo == nil {
		L.ArgError(2, "unsupported language: "+langAlias)
		return 0
	}

	if langInfo.Language == nil {
		L.ArgError(2, "language '"+langAlias+"' does not have a Tree-sitter language binding")
		return 0
	}

	lang := langInfo.Language()
	err := p.parser.SetLanguage(treesitter.NewLanguage(lang))
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

func parserGetLanguage(L *lua.LState) int {
	p := checkParser(L)
	lang := p.parser.Language()
	if lang == nil {
		L.Push(lua.LNil)
		return 1
	}

	L.Push(lua.LString("unknown"))
	return 1
}

func parserParse(L *lua.LState) int {
	parser := checkParser(L)
	code := L.CheckString(2)
	var oldTree *treesitter.Tree

	if parser.parser.Language() == nil {
		log.Println("language is not set")
		L.ArgError(1, "language is not set")
		return 2
	}

	if L.GetTop() > 2 {
		if ud := L.CheckUserData(3); ud != nil {
			if tw, ok := ud.Value.(*TreeWrapper); ok {
				oldTree = tw.tree
			} else {
				L.ArgError(2, "tree expected")
				return 0
			}
		}
	}

	if code == "" {
		L.Push(lua.LNil)
		L.Push(lua.LString("code is empty"))
		return 2
	}

	tree := parser.parser.Parse([]byte(code), oldTree)
	if tree == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("failed to parse code"))
		return 2
	}

	ud := L.NewUserData()
	ud.Value = &TreeWrapper{tree: tree}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Tree"))
	L.Push(ud)
	return 1
}

func parserGC(L *lua.LState) int {
	parser := checkParser(L)
	parser.Close()
	return 0
}

func (p *ParserWrapper) Close() {
	if p.parser != nil {
		p.parser.Close()
		p.parser = nil
	}
}

func checkParser(L *lua.LState) *ParserWrapper {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*ParserWrapper); ok {
		return v
	}
	L.ArgError(1, "Parser expected")
	return nil
}
