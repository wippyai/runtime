package treesitter

import (
	"context"
	"fmt"
	"time"

	"github.com/ponyruntime/pony/internal/closer"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	lua "github.com/yuin/gopher-lua"
)

type ParserWrapper struct {
	parser *treesitter.Parser
	lang   *LanguageInfo
}

func registerParser(L *lua.LState) {
	mt := L.NewTypeMetatable("treesitter.Parser")
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), map[string]lua.LGFunction{
		"parse":        parserParse,
		"set_language": parserSetLanguage,
		"get_language": parserGetLanguage,
		"reset":        parserReset,
		"close":        parserClose,
		"set_timeout":  parserSetTimeout,
		"set_ranges":   parserSetRanges,
	}))
}

func newParser(l *lua.LState) int {
	parser := treesitter.NewParser()
	wrap := &ParserWrapper{parser: parser}

	if l.Context() != nil {
		cleanup := closer.FromContext(l.Context())
		if cleanup != nil {
			cleanup.Add(func() error { wrap.Close(); return nil })
		}
	}

	ud := l.NewUserData()
	ud.Value = wrap
	l.SetMetatable(ud, l.GetTypeMetatable("treesitter.Parser"))
	l.Push(ud)
	return 1
}

func (p *ParserWrapper) parseWithContext(ctx context.Context, code []byte, oldTree *TreeWrapper) (*treesitter.Tree, error) {
	if deadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(deadline)
		p.parser.SetTimeoutMicros(uint64(timeout.Microseconds())) //nolint:gosec
	}

	var cflag uintptr
	p.parser.SetCancellationFlag(&cflag)

	var oldTreePtr *treesitter.Tree
	if oldTree != nil {
		oldTreePtr = oldTree.tree
	}

	tree := p.parser.ParseCtx(ctx, code, oldTreePtr)
	if tree == nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("failed to parse code")
	}

	return tree, nil
}

func parserSetLanguage(L *lua.LState) int {
	p := checkParser(L)
	langAlias := L.CheckString(2)

	langInfo := NewLanguages().GetLanguageInfo(langAlias)
	if langInfo == nil {
		L.RaiseError("language %s is not found", langAlias)
		return 0
	}

	if langInfo.Language == nil {
		L.RaiseError("language %s does not have a Tree-sitter language binding", langAlias)
		return 0
	}

	lang := langInfo.Language()
	err := p.parser.SetLanguage(treesitter.NewLanguage(lang))
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	p.lang = langInfo

	L.Push(lua.LTrue)
	return 1
}

// In parser.go:
func parserGetLanguage(L *lua.LState) int {
	p := checkParser(L)

	if p.lang == nil {
		L.RaiseError("language is not set")
		return 0
	}

	L.Push(lua.LString(p.lang.Name))
	return 1
}

func parserParse(L *lua.LState) int {
	parser := checkParser(L)
	code := L.CheckString(2)

	if parser.parser.Language() == nil {
		L.ArgError(1, "language is not set")
		return 0
	}

	var oldTree *TreeWrapper
	if L.GetTop() > 2 {
		if ud := L.CheckUserData(3); ud != nil {
			if tw, ok := ud.Value.(*TreeWrapper); ok {
				oldTree = tw
			} else {
				L.ArgError(3, "tree expected")
				return 0
			}
		}
	}

	// Get context from Lua state
	ctx := L.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Parse with context handling
	tree, err := parser.parseWithContext(ctx, []byte(code), oldTree)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	if L.Context() != nil {
		cleanup := closer.FromContext(L.Context())
		if cleanup != nil {
			cleanup.Add(func() error { tree.Close(); return nil })
		}
	}

	ud := L.NewUserData()
	ud.Value = &TreeWrapper{tree: tree, source: code}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Tree"))
	L.Push(ud)
	return 1
}

func parserReset(L *lua.LState) int {
	p := checkParser(L)
	p.parser.Reset()
	return 0
}

func parserClose(L *lua.LState) int {
	p := checkParser(L)
	p.Close()

	return 0
}

func parserSetTimeout(L *lua.LState) int {
	p := checkParser(L)
	timeout := L.CheckNumber(2)
	p.parser.SetTimeoutMicros(uint64(timeout * 1000000)) // Convert seconds to microseconds
	return 0
}

func parserSetRanges(L *lua.LState) int {
	p := checkParser(L)
	rangesTable := L.CheckTable(2)

	var ranges []treesitter.Range
	var parseError error

	rangesTable.ForEach(func(_, value lua.LValue) {
		if parseError != nil {
			return
		}

		if t, ok := value.(*lua.LTable); ok {
			var r treesitter.Range

			// Get values with explicit type checks
			if v := t.RawGetString("start_byte"); v.Type() == lua.LTNumber {
				r.StartByte = uint(v.(lua.LNumber))
			} else {
				parseError = fmt.Errorf("start_byte must be a number")
				return
			}

			if v := t.RawGetString("end_byte"); v.Type() == lua.LTNumber {
				r.EndByte = uint(v.(lua.LNumber))
			} else {
				parseError = fmt.Errorf("end_byte must be a number")
				return
			}

			if v := t.RawGetString("start_row"); v.Type() == lua.LTNumber {
				r.StartPoint.Row = uint(v.(lua.LNumber))
			} else {
				parseError = fmt.Errorf("start_row must be a number")
				return
			}

			if v := t.RawGetString("start_col"); v.Type() == lua.LTNumber {
				r.StartPoint.Column = uint(v.(lua.LNumber))
			} else {
				parseError = fmt.Errorf("start_col must be a number")
				return
			}

			if v := t.RawGetString("end_row"); v.Type() == lua.LTNumber {
				r.EndPoint.Row = uint(v.(lua.LNumber))
			} else {
				parseError = fmt.Errorf("end_row must be a number")
				return
			}

			if v := t.RawGetString("end_col"); v.Type() == lua.LTNumber {
				r.EndPoint.Column = uint(v.(lua.LNumber))
			} else {
				parseError = fmt.Errorf("end_col must be a number")
				return
			}

			ranges = append(ranges, r)
		}
	})

	if parseError != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(parseError.Error()))
		return 2
	}

	if err := p.SetIncludedRanges(ranges); err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

func (p *ParserWrapper) SetIncludedRanges(ranges []treesitter.Range) error {
	if p.parser == nil {
		return fmt.Errorf("parser is closed")
	}

	return p.parser.SetIncludedRanges(ranges)
}

func (p *ParserWrapper) Reset() {
	p.parser.Reset()
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
