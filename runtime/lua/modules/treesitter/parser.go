package treesitter

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	treesitter "github.com/tree-sitter/go-tree-sitter"
	lua "github.com/yuin/gopher-lua"
	"time"
)

// ParserWrapper wraps a Tree-sitter parser with language information
type ParserWrapper struct {
	parser  *treesitter.Parser
	lang    *LanguageInfo
	closed  bool
	release context.CancelFunc
}

func registerParser(l *lua.LState) {
	methods := map[string]lua.LGFunction{
		"parse":        parserParse,
		"set_language": parserSetLanguage,
		"get_language": parserGetLanguage,
		"reset":        parserReset,
		"close":        parserClose,
		"set_timeout":  parserSetTimeout,
		"set_ranges":   parserSetRanges,
	}
	value.RegisterMethods(l, "treesitter.Parser", methods)
}

// NewParser creates a new parser wrapper with proper UoW integration
func NewParser(uw engine.UnitOfWork, parser *treesitter.Parser) *ParserWrapper {
	wrapper := &ParserWrapper{
		parser: parser,
	}

	var flag uintptr
	parser.SetCancellationFlag(&flag)

	// Register cleanup with UoW, storing the cancel function
	wrapper.release = uw.AddCleanup(func() error {
		if wrapper.parser != nil && !wrapper.closed {
			wrapper.parser.Close()
			wrapper.parser = nil
			wrapper.closed = true
		}
		return nil
	})

	return wrapper
}

// SetIncludedRanges sets the ranges of text that should be included in parsing
func (p *ParserWrapper) SetIncludedRanges(ranges []treesitter.Range) error {
	if p.parser == nil {
		return fmt.Errorf("parser is closed")
	}
	return p.parser.SetIncludedRanges(ranges)
}

// Reset resets the parser to its initial state
func (p *ParserWrapper) Reset() {
	p.parser.Reset()
}

// Close marks the parser as closed and cancels the UoW cleanup
func (p *ParserWrapper) Close() {
	if !p.closed && p.release != nil {
		p.closed = true
		p.release() // Remove cleanup from UoW but don't execute it
		p.release = nil
	}
}

func newParser(l *lua.LState) int {
	parser := treesitter.NewParser()

	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("unit of work is not found")
		return 0
	}

	// Use the new constructor
	p := NewParser(uw, parser)

	ud := l.NewUserData()
	ud.Value = p
	ud.Metatable = value.GetTypeMetatable(l, "treesitter.Parser")
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

func parserSetLanguage(l *lua.LState) int {
	p := checkParser(l)
	langAlias := l.CheckString(2)

	langInfo := NewLanguages().GetLanguageInfo(langAlias)
	if langInfo == nil {
		l.RaiseError("language %s is not found", langAlias)
		return 0
	}

	if langInfo.Language == nil {
		l.RaiseError("language %s does not have a Tree-sitter language binding", langAlias)
		return 0
	}

	lang := langInfo.Language()
	err := p.parser.SetLanguage(treesitter.NewLanguage(lang))
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	p.lang = langInfo

	l.Push(lua.LTrue)
	return 1
}

func parserGetLanguage(l *lua.LState) int {
	p := checkParser(l)

	if p.lang == nil {
		l.RaiseError("language is not set")
		return 0
	}

	l.Push(lua.LString(p.lang.Name))
	return 1
}

func parserParse(l *lua.LState) int {
	parser := checkParser(l)
	code := l.CheckString(2)

	if parser.parser.Language() == nil {
		l.ArgError(1, "language is not set")
		return 0
	}

	var oldTree *TreeWrapper
	if l.GetTop() > 2 {
		if ud := l.CheckUserData(3); ud != nil {
			if tw, ok := ud.Value.(*TreeWrapper); ok {
				oldTree = tw
			} else {
				l.ArgError(3, "tree expected")
				return 0
			}
		}
	}

	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("unit of work is not found")
		return 0
	}

	tree, err := parser.parseWithContext(uw.Context(), []byte(code), oldTree)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	tw := NewTree(uw, tree, code)

	ud := l.NewUserData()
	ud.Value = tw
	ud.Metatable = value.GetTypeMetatable(l, "treesitter.Tree")

	l.Push(ud)
	return 1
}

func parserReset(l *lua.LState) int {
	p := checkParser(l)
	p.parser.Reset()
	return 0
}

func parserClose(l *lua.LState) int {
	p := checkParser(l)
	p.Close()
	return 0
}

func parserSetTimeout(l *lua.LState) int {
	p := checkParser(l)
	timeout := l.CheckNumber(2)
	p.parser.SetTimeoutMicros(uint64(timeout * 1000000)) // Convert seconds to microseconds
	return 0
}

func parserSetRanges(l *lua.LState) int {
	p := checkParser(l)
	rangesTable := l.CheckTable(2)

	var ranges []treesitter.Range
	var parseError error

	rangesTable.ForEach(func(_, value lua.LValue) {
		if parseError != nil {
			return
		}

		if t, ok := value.(*lua.LTable); ok {
			var r treesitter.Range

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
		l.Push(lua.LFalse)
		l.Push(lua.LString(parseError.Error()))
		return 2
	}

	if err := p.SetIncludedRanges(ranges); err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

func checkParser(l *lua.LState) *ParserWrapper {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*ParserWrapper); ok {
		if v.closed {
			l.ArgError(1, "parser already closed")
			return nil
		}
		return v
	}
	l.ArgError(1, "Parser expected")
	return nil
}
