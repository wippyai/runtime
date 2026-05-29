// SPDX-License-Identifier: MPL-2.0

//go:build treesitter

package treesitter

import (
	"context"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const typeCursor = "treesitter.Cursor"

// pushCursor pushes a Cursor userdata to the stack
func pushCursor(l *lua.LState, cw *CursorWrapper) {
	value.PushTypedUserData(l, cw, typeCursor)
}

// CursorWrapper wraps a tree-sitter TreeCursor for Lua integration
type CursorWrapper struct {
	cursor        *treesitter.TreeCursor
	source        *string
	cancelCleanup func()
	closed        bool
}

func NewCursor(ctx context.Context, cursor *treesitter.TreeCursor, source *string) *CursorWrapper {
	wrapper := &CursorWrapper{
		cursor: cursor,
		source: source,
	}

	// Register cleanup with resource store
	store := resource.GetStore(ctx)
	if store != nil {
		wrapper.cancelCleanup = store.AddCleanup(func() error {
			if wrapper.cursor != nil && !wrapper.closed {
				wrapper.cursor.Close()
				wrapper.cursor = nil
				wrapper.closed = true
			}
			return nil
		})
	}

	return wrapper
}

func (c *CursorWrapper) Close() {
	if !c.closed && c.cancelCleanup != nil {
		c.closed = true
		c.cancelCleanup()
		c.cancelCleanup = nil
	}
}

func cursorCurrentNode(l *lua.LState) int {
	cursor := checkCursor(l)
	if cursor == nil {
		return 0
	}
	node := cursor.cursor.Node()
	if node == nil {
		l.Push(lua.LNil)
		return 1
	}

	pushNode(l, node, cursor.source)
	return 1
}

func cursorCurrentFieldID(l *lua.LState) int {
	cursor := checkCursor(l)
	if cursor == nil {
		return 0
	}
	l.Push(lua.LNumber(cursor.cursor.FieldId()))
	return 1
}

func cursorCurrentFieldName(l *lua.LState) int {
	cursor := checkCursor(l)
	if cursor == nil {
		return 0
	}
	fieldName := cursor.cursor.FieldName()
	if fieldName == "" {
		l.Push(lua.LNil)
		return 1
	}
	l.Push(lua.LString(fieldName))
	return 1
}

func cursorCurrentDepth(l *lua.LState) int {
	cursor := checkCursor(l)
	if cursor == nil {
		return 0
	}
	l.Push(lua.LNumber(cursor.cursor.Depth()))
	return 1
}

func cursorCurrentDescendantIndex(l *lua.LState) int {
	cursor := checkCursor(l)
	if cursor == nil {
		return 0
	}
	l.Push(lua.LNumber(cursor.cursor.DescendantIndex()))
	return 1
}

func cursorGotoParent(l *lua.LState) int {
	cursor := checkCursor(l)
	if cursor == nil {
		return 0
	}
	success := cursor.cursor.GotoParent()
	l.Push(lua.LBool(success))
	return 1
}

func cursorGotoFirstChild(l *lua.LState) int {
	cursor := checkCursor(l)
	if cursor == nil {
		return 0
	}
	success := cursor.cursor.GotoFirstChild()
	l.Push(lua.LBool(success))
	return 1
}

func cursorGotoLastChild(l *lua.LState) int {
	cursor := checkCursor(l)
	if cursor == nil {
		return 0
	}
	success := cursor.cursor.GotoLastChild()
	l.Push(lua.LBool(success))
	return 1
}

func cursorGotoNextSibling(l *lua.LState) int {
	cursor := checkCursor(l)
	if cursor == nil {
		return 0
	}
	success := cursor.cursor.GotoNextSibling()
	l.Push(lua.LBool(success))
	return 1
}

func cursorGotoPreviousSibling(l *lua.LState) int {
	cursor := checkCursor(l)
	if cursor == nil {
		return 0
	}
	success := cursor.cursor.GotoPreviousSibling()
	l.Push(lua.LBool(success))
	return 1
}

func cursorGotoDescendant(l *lua.LState) int {
	cursor := checkCursor(l)
	if cursor == nil {
		return 0
	}
	index := uint32(l.CheckNumber(2))
	cursor.cursor.GotoDescendant(index)
	return 0
}

func cursorGotoFirstChildForByte(l *lua.LState) int {
	cursor := checkCursor(l)
	if cursor == nil {
		return 0
	}
	byteIndex := uint32(l.CheckNumber(2))
	if index := cursor.cursor.GotoFirstChildForByte(byteIndex); index != nil {
		l.Push(lua.LNumber(*index))
	} else {
		l.Push(lua.LNil)
	}
	return 1
}

func cursorGotoFirstChildForPoint(l *lua.LState) int {
	cursor := checkCursor(l)
	if cursor == nil {
		return 0
	}

	// Spawn point table argument
	pointTbl := l.CheckTable(2)
	row := toUint(pointTbl.RawGetString("row"))
	col := toUint(pointTbl.RawGetString("column"))

	point := treesitter.Point{Row: row, Column: col}
	if index := cursor.cursor.GotoFirstChildForPoint(point); index != nil {
		l.Push(lua.LNumber(*index))
	} else {
		l.Push(lua.LNil)
	}
	return 1
}

func cursorReset(l *lua.LState) int {
	cursor := checkCursor(l)
	if cursor == nil {
		return 0
	}
	ud := l.CheckUserData(2)
	if node, ok := ud.Value.(*NodeWrapper); ok {
		cursor.cursor.Reset(*node.node)
		return 0
	}
	return 0
}

func cursorResetTo(l *lua.LState) int {
	cursor := checkCursor(l)
	if cursor == nil {
		return 0
	}
	ud := l.CheckUserData(2)
	if otherCursor, ok := ud.Value.(*CursorWrapper); ok {
		cursor.cursor.ResetTo(otherCursor.cursor)
		return 0
	}
	l.ArgError(2, "TreeCursor expected")
	return 0
}

func cursorCopy(l *lua.LState) int {
	cursor := checkCursor(l)
	if cursor == nil {
		return 0
	}
	copied := cursor.cursor.Copy()

	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context found").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	pushCursor(l, NewCursor(ctx, copied, cursor.source))
	return 1
}

func cursorClose(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*CursorWrapper); ok {
		v.Close()
	}
	return 0
}

// Helper functions
func checkCursor(l *lua.LState) *CursorWrapper {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*CursorWrapper); ok {
		if v.closed || v.cursor == nil {
			l.ArgError(1, "TreeCursor is closed")
			return nil
		}
		return v
	}
	l.ArgError(1, "TreeCursor expected")
	return nil
}
