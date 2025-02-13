package treesitter

import (
	treesitter "github.com/tree-sitter/go-tree-sitter"
	lua "github.com/yuin/gopher-lua"
)

// CursorWrapper wraps a tree-sitter TreeCursor for Lua integration
type CursorWrapper struct {
	cursor *treesitter.TreeCursor
	source *string
}

// Register the Cursor type to Lua
func registerCursor(l *lua.LState) {
	mt := l.NewTypeMetatable("treesitter.Cursor")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"current_node":               cursorCurrentNode,
		"current_field_id":           cursorCurrentFieldID,
		"current_field_name":         cursorCurrentFieldName,
		"current_depth":              cursorCurrentDepth,
		"current_descendant_index":   cursorCurrentDescendantIndex,
		"goto_parent":                cursorGotoParent,
		"goto_first_child":           cursorGotoFirstChild,
		"goto_last_child":            cursorGotoLastChild,
		"goto_next_sibling":          cursorGotoNextSibling,
		"goto_previous_sibling":      cursorGotoPreviousSibling,
		"goto_descendant":            cursorGotoDescendant,
		"goto_first_child_for_byte":  cursorGotoFirstChildForByte,
		"goto_first_child_for_point": cursorGotoFirstChildForPoint,
		"reset":                      cursorReset,
		"reset_to":                   cursorResetTo,
		"copy":                       cursorCopy,
		"close":                      cursorClose,
	}))
}

func cursorCurrentNode(l *lua.LState) int {
	cursor := checkCursor(l)
	node := cursor.cursor.Node()
	if node == nil {
		l.Push(lua.LNil)
		return 1
	}

	ud := l.NewUserData()
	ud.Value = &NodeWrapper{node: node, source: cursor.source}
	l.SetMetatable(ud, l.GetTypeMetatable("treesitter.Node"))
	l.Push(ud)
	return 1
}

func cursorCurrentFieldID(l *lua.LState) int {
	cursor := checkCursor(l)
	l.Push(lua.LNumber(cursor.cursor.FieldId()))
	return 1
}

func cursorCurrentFieldName(l *lua.LState) int {
	cursor := checkCursor(l)
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
	l.Push(lua.LNumber(cursor.cursor.Depth()))
	return 1
}

func cursorCurrentDescendantIndex(l *lua.LState) int {
	cursor := checkCursor(l)
	l.Push(lua.LNumber(cursor.cursor.DescendantIndex()))
	return 1
}

func cursorGotoParent(l *lua.LState) int {
	cursor := checkCursor(l)
	success := cursor.cursor.GotoParent()
	l.Push(lua.LBool(success))
	return 1
}

func cursorGotoFirstChild(l *lua.LState) int {
	cursor := checkCursor(l)
	success := cursor.cursor.GotoFirstChild()
	l.Push(lua.LBool(success))
	return 1
}

func cursorGotoLastChild(l *lua.LState) int {
	cursor := checkCursor(l)
	success := cursor.cursor.GotoLastChild()
	l.Push(lua.LBool(success))
	return 1
}

func cursorGotoNextSibling(l *lua.LState) int {
	cursor := checkCursor(l)
	success := cursor.cursor.GotoNextSibling()
	l.Push(lua.LBool(success))
	return 1
}

func cursorGotoPreviousSibling(l *lua.LState) int {
	cursor := checkCursor(l)
	success := cursor.cursor.GotoPreviousSibling()
	l.Push(lua.LBool(success))
	return 1
}

func cursorGotoDescendant(l *lua.LState) int {
	cursor := checkCursor(l)
	index := uint32(l.CheckNumber(2))
	cursor.cursor.GotoDescendant(index)
	return 0
}

func cursorGotoFirstChildForByte(l *lua.LState) int {
	cursor := checkCursor(l)
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

	// Spawn point table argument
	pointTbl := l.CheckTable(2)
	row := uint(pointTbl.RawGetString("row").(lua.LNumber))
	col := uint(pointTbl.RawGetString("column").(lua.LNumber))

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
	ud := l.CheckUserData(2)
	if node, ok := ud.Value.(*NodeWrapper); ok {
		cursor.cursor.Reset(*node.node)
		return 0
	}
	return 0
}

func cursorResetTo(l *lua.LState) int {
	cursor := checkCursor(l)
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
	copied := cursor.cursor.Copy()

	ud := l.NewUserData()
	ud.Value = &CursorWrapper{cursor: copied, source: cursor.source}
	l.SetMetatable(ud, l.GetTypeMetatable("treesitter.Cursor"))
	l.Push(ud)
	return 1
}

func cursorClose(l *lua.LState) int {
	cursor := checkCursor(l)
	if cursor != nil {
		cursor.cursor.Close()
	}
	return 0
}

// Helper functions
func checkCursor(l *lua.LState) *CursorWrapper {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*CursorWrapper); ok {
		return v
	}
	l.ArgError(1, "TreeCursor expected")
	return nil
}
