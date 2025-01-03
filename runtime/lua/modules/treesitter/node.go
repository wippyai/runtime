package treesitter

import (
	treesitter "github.com/tree-sitter/go-tree-sitter"
	"github.com/yuin/gopher-lua"
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
	"parent":                           nodeParent,
	"child":                            nodeChild,
	"child_count":                      nodeChildCount,
	"next_sibling":                     nodeNextSibling,
	"prev_sibling":                     nodePrevSibling,
	"next_named_sibling":               nodeNextNamedSibling,
	"prev_named_sibling":               nodePrevNamedSibling,
	"named_child":                      nodeNamedChild,
	"named_child_count":                nodeNamedChildCount,
	"named_descendant_for_point_range": nodeNamedDescendantForPointRange,
	"descendant_count":                 nodeDescendantCount,

	// Field-related methods
	"child_by_field_name":  nodeChildByFieldName,
	"field_name_for_child": nodeFieldNameForChild,

	// Inspection methods
	"kind":         nodeKind,
	"is_named":     nodeIsNamed,
	"grammar_name": nodeGrammarName,
	"is_extra":     nodeIsExtra,
	"is_missing":   nodeIsMissing,

	"has_error": nodeHasError,
	"is_error":  nodeIsError,

	// Position methods
	"start_byte":  nodeStartByte,
	"end_byte":    nodeEndByte,
	"start_point": nodeStartPoint,
	"end_point":   nodeEndPoint,

	// Text and source methods
	"text":    nodeText,
	"to_sexp": nodeToSexp}

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

// nodeText retrieves the source text for this node
func nodeText(L *lua.LState) int {
	node := checkNode(L)
	source := L.CheckString(2)

	// Get byte positions
	start := node.node.StartByte()
	end := node.node.EndByte()

	// Bounds checking with uint32 for safe comparisons
	sourceLen := uint32(len(source))
	startPos := uint32(start)
	endPos := uint32(end)

	if start < 0 || end < 0 || startPos > endPos || endPos > sourceLen {
		// Instead of just returning nil, raise an error
		L.RaiseError("invalid byte range")
		return 0 // This line won't be reached due to RaiseError
	}

	// Extract text
	text := source[start:end]
	L.Push(lua.LString(text))
	return 1
}

func nodeGrammarName(L *lua.LState) int {
	node := checkNode(L)
	grammarName := node.node.GrammarName()
	L.Push(lua.LString(grammarName))
	return 1
}

func nodeIsExtra(L *lua.LState) int {
	node := checkNode(L)
	L.Push(lua.LBool(node.node.IsExtra()))
	return 1
}

func nodeIsMissing(L *lua.LState) int {
	node := checkNode(L)
	L.Push(lua.LBool(node.node.IsMissing()))
	return 1
}

func nodeNamedDescendantForPointRange(L *lua.LState) int {
	node := checkNode(L)

	// Get start point table argument
	startPointTbl := L.CheckTable(2)
	startRow := uint(startPointTbl.RawGetString("row").(lua.LNumber))
	startCol := uint(startPointTbl.RawGetString("column").(lua.LNumber))

	// Get end point table argument
	endPointTbl := L.CheckTable(3)
	endRow := uint(endPointTbl.RawGetString("row").(lua.LNumber))
	endCol := uint(endPointTbl.RawGetString("column").(lua.LNumber))

	startPoint := treesitter.Point{Row: startRow, Column: startCol}
	endPoint := treesitter.Point{Row: endRow, Column: endCol}

	descendant := node.node.NamedDescendantForPointRange(startPoint, endPoint)
	if descendant == nil {
		L.Push(lua.LNil)
		return 1
	}

	ud := L.NewUserData()
	ud.Value = &NodeWrapper{node: descendant}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Node"))
	L.Push(ud)
	return 1
}

func nodeDescendantCount(L *lua.LState) int {
	node := checkNode(L)
	count := node.node.DescendantCount()
	L.Push(lua.LNumber(count))
	return 1
}

func nodeToSexp(L *lua.LState) int {
	node := checkNode(L)
	sexp := node.node.ToSexp()
	L.Push(lua.LString(sexp))
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
