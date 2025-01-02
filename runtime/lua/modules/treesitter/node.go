package treesitter

import (
	"github.com/ponyruntime/go-lua"
	treesitter "github.com/tree-sitter/go-tree-sitter"
)

// NodeWrapper wraps a tree-sitter Node for Lua integration
type NodeWrapper struct {
	node *treesitter.Node
}

// Register the Node type to Lua
func registerNode(L *lua.LState) {
	mt := L.NewTypeMetatable("treesitter.Node")
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), nodeMethods))
}

var nodeMethods = map[string]lua.LGFunction{
	// Navigation methods
	"parent":             nodeParent,
	"child":              nodeChild,
	"child_count":        nodeChildCount,
	"next_sibling":       nodeNextSibling,
	"prev_sibling":       nodePrevSibling,
	"next_named_sibling": nodeNextNamedSibling,
	"prev_named_sibling": nodePrevNamedSibling,
	"named_child":        nodeNamedChild,
	"named_child_count":  nodeNamedChildCount,

	// Field-related methods
	"child_by_field_name":  nodeChildByFieldName,
	"field_name_for_child": nodeFieldNameForChild,

	// Inspection methods
	"kind":      nodeKind,
	"is_named":  nodeIsNamed,
	"has_error": nodeHasError,
	"is_error":  nodeIsError,

	// Position methods
	"start_byte":  nodeStartByte,
	"end_byte":    nodeEndByte,
	"start_point": nodeStartPoint,
	"end_point":   nodeEndPoint,
}

// Navigation methods implementation

func nodeParent(L *lua.LState) int {
	node := checkNode(L)
	parent := node.node.Parent()
	if parent == nil {
		L.Push(lua.LNil)
		return 1
	}
	ud := L.NewUserData()
	ud.Value = &NodeWrapper{node: parent}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Node"))
	L.Push(ud)
	return 1
}

func nodeChild(L *lua.LState) int {
	node := checkNode(L)
	idx := uint(L.CheckNumber(2))
	child := node.node.Child(idx)
	if child == nil {
		L.Push(lua.LNil)
		return 1
	}
	ud := L.NewUserData()
	ud.Value = &NodeWrapper{node: child}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Node"))
	L.Push(ud)
	return 1
}

func nodeChildCount(L *lua.LState) int {
	node := checkNode(L)
	L.Push(lua.LNumber(node.node.ChildCount()))
	return 1
}

func nodeNextSibling(L *lua.LState) int {
	node := checkNode(L)
	sibling := node.node.NextSibling()
	if sibling == nil {
		L.Push(lua.LNil)
		return 1
	}
	ud := L.NewUserData()
	ud.Value = &NodeWrapper{node: sibling}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Node"))
	L.Push(ud)
	return 1
}

func nodePrevSibling(L *lua.LState) int {
	node := checkNode(L)
	sibling := node.node.PrevSibling()
	if sibling == nil {
		L.Push(lua.LNil)
		return 1
	}
	ud := L.NewUserData()
	ud.Value = &NodeWrapper{node: sibling}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Node"))
	L.Push(ud)
	return 1
}

func nodeNextNamedSibling(L *lua.LState) int {
	node := checkNode(L)
	sibling := node.node.NextNamedSibling()
	if sibling == nil {
		L.Push(lua.LNil)
		return 1
	}
	ud := L.NewUserData()
	ud.Value = &NodeWrapper{node: sibling}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Node"))
	L.Push(ud)
	return 1
}

func nodePrevNamedSibling(L *lua.LState) int {
	node := checkNode(L)
	sibling := node.node.PrevNamedSibling()
	if sibling == nil {
		L.Push(lua.LNil)
		return 1
	}
	ud := L.NewUserData()
	ud.Value = &NodeWrapper{node: sibling}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Node"))
	L.Push(ud)
	return 1
}

func nodeNamedChild(L *lua.LState) int {
	node := checkNode(L)
	idx := uint(L.CheckNumber(2))
	child := node.node.NamedChild(idx)
	if child == nil {
		L.Push(lua.LNil)
		return 1
	}
	ud := L.NewUserData()
	ud.Value = &NodeWrapper{node: child}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Node"))
	L.Push(ud)
	return 1
}

func nodeNamedChildCount(L *lua.LState) int {
	node := checkNode(L)
	L.Push(lua.LNumber(node.node.NamedChildCount()))
	return 1
}

// Field-related methods implementation

func nodeChildByFieldName(L *lua.LState) int {
	node := checkNode(L)
	fieldName := L.CheckString(2)
	child := node.node.ChildByFieldName(fieldName)
	if child == nil {
		L.Push(lua.LNil)
		return 1
	}
	ud := L.NewUserData()
	ud.Value = &NodeWrapper{node: child}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Node"))
	L.Push(ud)
	return 1
}

func nodeFieldNameForChild(L *lua.LState) int {
	node := checkNode(L)
	idx := uint32(L.CheckNumber(2))
	fieldName := node.node.FieldNameForChild(idx)
	if fieldName == "" {
		L.Push(lua.LNil)
		return 1
	}
	L.Push(lua.LString(fieldName))
	return 1
}

// Inspection methods implementation

func nodeKind(L *lua.LState) int {
	node := checkNode(L)
	L.Push(lua.LString(node.node.Kind()))
	return 1
}

func nodeIsNamed(L *lua.LState) int {
	node := checkNode(L)
	L.Push(lua.LBool(node.node.IsNamed()))
	return 1
}

func nodeHasError(L *lua.LState) int {
	node := checkNode(L)
	L.Push(lua.LBool(node.node.HasError()))
	return 1
}

func nodeIsError(L *lua.LState) int {
	node := checkNode(L)
	L.Push(lua.LBool(node.node.IsError()))
	return 1
}

// Position methods implementation

func nodeStartByte(L *lua.LState) int {
	node := checkNode(L)
	L.Push(lua.LNumber(node.node.StartByte()))
	return 1
}

func nodeEndByte(L *lua.LState) int {
	node := checkNode(L)
	L.Push(lua.LNumber(node.node.EndByte()))
	return 1
}

func nodeStartPoint(L *lua.LState) int {
	node := checkNode(L)
	point := node.node.StartPosition()
	pointTable := L.NewTable()
	pointTable.RawSetString("row", lua.LNumber(point.Row))
	pointTable.RawSetString("column", lua.LNumber(point.Column))
	L.Push(pointTable)
	return 1
}

func nodeEndPoint(L *lua.LState) int {
	node := checkNode(L)
	point := node.node.EndPosition()
	pointTable := L.NewTable()
	pointTable.RawSetString("row", lua.LNumber(point.Row))
	pointTable.RawSetString("column", lua.LNumber(point.Column))
	L.Push(pointTable)
	return 1
}

// Helper functions

func checkNode(L *lua.LState) *NodeWrapper {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*NodeWrapper); ok {
		return v
	}

	L.ArgError(1, "Node expected")
	return nil
}
