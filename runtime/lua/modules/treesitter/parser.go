// SPDX-License-Identifier: MPL-2.0

//go:build treesitter

package treesitter

import (
	"context"
	"fmt"
	"time"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const typeParser = "treesitter.Parser"

// pushParser pushes a Parser userdata to the stack
func pushParser(l *lua.LState, p *ParserWrapper) {
	value.PushTypedUserData(l, p, typeParser)
}

// ParserWrapper wraps a Tree-sitter parser with language information
type ParserWrapper struct {
	parser        *treesitter.Parser
	lang          *LanguageInfo
	cancelCleanup func()
	timeout       time.Duration
	closed        bool
}

// NewParser creates a new parser wrapper with proper resource store integration
func NewParser(ctx context.Context, parser *treesitter.Parser) *ParserWrapper {
	wrapper := &ParserWrapper{
		parser: parser,
	}

	// Register cleanup with resource store
	store := resource.GetStore(ctx)
	if store != nil {
		wrapper.cancelCleanup = store.AddCleanup(func() error {
			if wrapper.parser != nil && !wrapper.closed {
				wrapper.parser.Close()
				wrapper.parser = nil
				wrapper.closed = true
			}
			return nil
		})
	}

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

// Close marks the parser as closed and cancels the cleanup
func (p *ParserWrapper) Close() {
	if !p.closed && p.cancelCleanup != nil {
		p.closed = true
		p.cancelCleanup()
		p.cancelCleanup = nil
	}
}

func newParser(l *lua.LState) int {
	parser := treesitter.NewParser()

	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context found").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	pushParser(l, NewParser(ctx, parser))
	return 1
}

func (p *ParserWrapper) parseWithContext(ctx context.Context, code []byte, oldTree *TreeWrapper) (*treesitter.Tree, error) {
	var oldTreePtr *treesitter.Tree
	if oldTree != nil {
		oldTreePtr = oldTree.tree
	}

	readCallback := func(offset int, _ treesitter.Point) []byte {
		if offset >= len(code) {
			return nil
		}
		return code[offset:]
	}

	var deadline time.Time
	if p.timeout > 0 {
		deadline = time.Now().Add(p.timeout)
	}

	opts := &treesitter.ParseOptions{
		ProgressCallback: func(_ treesitter.ParseState) bool {
			if ctx.Err() != nil {
				return true
			}
			if !deadline.IsZero() && time.Now().After(deadline) {
				return true
			}
			return false
		},
	}

	tree := p.parser.ParseWithOptions(readCallback, oldTreePtr, opts)
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
	if p == nil {
		return 0
	}
	langAlias := l.CheckString(2)

	langInfo := languages.GetLanguageInfo(langAlias)
	if langInfo == nil {
		err := lua.NewLuaError(l, fmt.Sprintf("language %s is not found", langAlias)).
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LFalse)
		l.Push(err)
		return 2
	}

	if langInfo.Language == nil {
		err := lua.NewLuaError(l, fmt.Sprintf("language %s does not have a Tree-sitter language binding", langAlias)).
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LFalse)
		l.Push(err)
		return 2
	}

	lang := langInfo.Language()
	if setErr := p.parser.SetLanguage(treesitter.NewLanguage(lang)); setErr != nil {
		err := lua.WrapErrorWithLua(l, setErr, "failed to set language").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LFalse)
		l.Push(err)
		return 2
	}
	p.lang = langInfo

	l.Push(lua.LTrue)
	return 1
}

func parserGetLanguage(l *lua.LState) int {
	p := checkParser(l)
	if p == nil {
		return 0
	}

	if p.lang == nil {
		err := lua.NewLuaError(l, "language is not set").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	l.Push(lua.LString(p.lang.Name))
	return 1
}

func parserParse(l *lua.LState) int {
	parser := checkParser(l)
	if parser == nil {
		return 0
	}
	code := l.CheckString(2)

	if parser.parser.Language() == nil {
		err := lua.NewLuaError(l, "language is not set").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	var oldTree *TreeWrapper
	if l.GetTop() > 2 {
		if ud := l.CheckUserData(3); ud != nil {
			if tw, ok := ud.Value.(*TreeWrapper); ok {
				oldTree = tw
			} else {
				err := lua.NewLuaError(l, "tree expected").
					WithKind(lua.Invalid).
					WithRetryable(false)
				l.Push(lua.LNil)
				l.Push(err)
				return 2
			}
		}
	}

	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context found").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	tree, parseErr := parser.parseWithContext(ctx, []byte(code), oldTree)
	if parseErr != nil {
		err := lua.WrapErrorWithLua(l, parseErr, "parse failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	tw := NewTree(ctx, tree, code)
	value.PushTypedUserData(l, tw, typeTree)
	return 1
}

func parserReset(l *lua.LState) int {
	p := checkParser(l)
	if p == nil {
		return 0
	}
	p.parser.Reset()
	return 0
}

func parserClose(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*ParserWrapper); ok {
		v.Close()
	}
	return 0
}

func parserSetTimeout(l *lua.LState) int {
	p := checkParser(l)
	if p == nil {
		return 0
	}
	duration, err := parseDuration(l, 2)
	if err != nil {
		l.ArgError(2, err.Error())
		return 0
	}
	p.timeout = duration
	return 0
}

func parserSetRanges(l *lua.LState) int {
	p := checkParser(l)
	if p == nil {
		return 0
	}
	rangesTable := l.CheckTable(2)

	var ranges []treesitter.Range
	var parseError error

	rangesTable.ForEach(func(_, value lua.LValue) {
		if parseError != nil {
			return
		}

		if t, ok := value.(*lua.LTable); ok {
			var r treesitter.Range

			if v := t.RawGetString("start_byte"); isNumber(v) {
				r.StartByte = toUint(v)
			} else {
				parseError = fmt.Errorf("start_byte must be a number")
				return
			}

			if v := t.RawGetString("end_byte"); isNumber(v) {
				r.EndByte = toUint(v)
			} else {
				parseError = fmt.Errorf("end_byte must be a number")
				return
			}

			if v := t.RawGetString("start_row"); isNumber(v) {
				r.StartPoint.Row = toUint(v)
			} else {
				parseError = fmt.Errorf("start_row must be a number")
				return
			}

			if v := t.RawGetString("start_col"); isNumber(v) {
				r.StartPoint.Column = toUint(v)
			} else {
				parseError = fmt.Errorf("start_col must be a number")
				return
			}

			if v := t.RawGetString("end_row"); isNumber(v) {
				r.EndPoint.Row = toUint(v)
			} else {
				parseError = fmt.Errorf("end_row must be a number")
				return
			}

			if v := t.RawGetString("end_col"); isNumber(v) {
				r.EndPoint.Column = toUint(v)
			} else {
				parseError = fmt.Errorf("end_col must be a number")
				return
			}

			ranges = append(ranges, r)
		}
	})

	if parseError != nil {
		err := lua.NewLuaError(l, parseError.Error()).
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LFalse)
		l.Push(err)
		return 2
	}

	if setErr := p.SetIncludedRanges(ranges); setErr != nil {
		err := lua.WrapErrorWithLua(l, setErr, "failed to set ranges").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LFalse)
		l.Push(err)
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
