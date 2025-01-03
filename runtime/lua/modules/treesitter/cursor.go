package treesitter

import (
	treesitter "github.com/tree-sitter/go-tree-sitter"
	"github.com/yuin/gopher-lua"
)

// Register the Cursor type to Lua
func registerCursor(L *lua.LState) {
	mt := L.NewTypeMetatable("treesitter.Cursor")
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), cursorMethods))
	L.SetField(mt, "__gc", L.NewFunction(cursorGC))
}

var cursorMethods = map[string]lua.LGFunction{
	"current_node":               cursorCurrentNode,
	"current_field_id":           cursorCurrentFieldId,
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
}

func cursorCurrentNode(L *lua.LState) int {
	cursor := checkCursor(L)
	node := cursor.Node()
	if node == nil {
		L.Push(lua.LNil)
		return 1
	}

	ud := L.NewUserData()
	ud.Value = &NodeWrapper{node: node}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Node"))
	L.Push(ud)
	return 1
}

func cursorCurrentFieldId(L *lua.LState) int {
	cursor := checkCursor(L)
	L.Push(lua.LNumber(cursor.FieldId()))
	return 1
}

func cursorCurrentFieldName(L *lua.LState) int {
	cursor := checkCursor(L)
	fieldName := cursor.FieldName()
	if fieldName == "" {
		L.Push(lua.LNil)
		return 1
	}
	L.Push(lua.LString(fieldName))
	return 1
}

func cursorCurrentDepth(L *lua.LState) int {
	cursor := checkCursor(L)
	L.Push(lua.LNumber(cursor.Depth()))
	return 1
}

func cursorCurrentDescendantIndex(L *lua.LState) int {
	cursor := checkCursor(L)
	L.Push(lua.LNumber(cursor.DescendantIndex()))
	return 1
}

func cursorGotoParent(L *lua.LState) int {
	cursor := checkCursor(L)
	success := cursor.GotoParent()
	L.Push(lua.LBool(success))
	return 1
}

func cursorGotoFirstChild(L *lua.LState) int {
	cursor := checkCursor(L)
	success := cursor.GotoFirstChild()
	L.Push(lua.LBool(success))
	return 1
}

func cursorGotoLastChild(L *lua.LState) int {
	cursor := checkCursor(L)
	success := cursor.GotoLastChild()
	L.Push(lua.LBool(success))
	return 1
}

func cursorGotoNextSibling(L *lua.LState) int {
	cursor := checkCursor(L)
	success := cursor.GotoNextSibling()
	L.Push(lua.LBool(success))
	return 1
}

func cursorGotoPreviousSibling(L *lua.LState) int {
	cursor := checkCursor(L)
	success := cursor.GotoPreviousSibling()
	L.Push(lua.LBool(success))
	return 1
}

func cursorGotoDescendant(L *lua.LState) int {
	cursor := checkCursor(L)
	index := uint32(L.CheckNumber(2))
	cursor.GotoDescendant(index)
	return 0
}

func cursorGotoFirstChildForByte(L *lua.LState) int {
	cursor := checkCursor(L)
	byteIndex := uint32(L.CheckNumber(2))
	if index := cursor.GotoFirstChildForByte(byteIndex); index != nil {
		L.Push(lua.LNumber(*index))
	} else {
		L.Push(lua.LNil)
	}
	return 1
}

func cursorGotoFirstChildForPoint(L *lua.LState) int {
	cursor := checkCursor(L)

	// Get point table argument
	pointTbl := L.CheckTable(2)
	row := uint(pointTbl.RawGetString("row").(lua.LNumber))
	col := uint(pointTbl.RawGetString("column").(lua.LNumber))

	point := treesitter.Point{Row: row, Column: col}
	if index := cursor.GotoFirstChildForPoint(point); index != nil {
		L.Push(lua.LNumber(*index))
	} else {
		L.Push(lua.LNil)
	}
	return 1
}

func cursorReset(L *lua.LState) int {
	cursor := checkCursor(L)
	ud := L.CheckUserData(2)
	if node, ok := ud.Value.(*NodeWrapper); ok {
		cursor.Reset(*node.node)
		return 0
	}
	return 0
}

func cursorResetTo(L *lua.LState) int {
	cursor := checkCursor(L)
	ud := L.CheckUserData(2)
	if otherCursor, ok := ud.Value.(*treesitter.TreeCursor); ok {
		cursor.ResetTo(otherCursor)
		return 0
	}
	L.ArgError(2, "TreeCursor expected")
	return 0
}

func cursorCopy(L *lua.LState) int {
	cursor := checkCursor(L)
	copied := cursor.Copy()

	ud := L.NewUserData()
	ud.Value = copied
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Cursor"))
	L.Push(ud)
	return 1
}

func cursorGC(L *lua.LState) int {
	cursor := checkCursor(L)
	if cursor != nil {
		cursor.Close()
	}
	return 0
}

// Helper functions
func checkCursor(L *lua.LState) *treesitter.TreeCursor {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*treesitter.TreeCursor); ok {
		return v
	}
	L.ArgError(1, "TreeCursor expected")
	return nil
}
