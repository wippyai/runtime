package treesitter

import (
	treesitter "github.com/tree-sitter/go-tree-sitter"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// NodeWrapper wraps a tree-sitter Node for Lua integration
type NodeWrapper struct {
	node   *treesitter.Node
	source *string
}

// Register the Node type to Lua
func registerNode(l *lua.LState) {
	methods := map[string]lua.LGFunction{
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
		"kind": nodeKind,
		"type": nodeKind, // alias for AI

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
		"to_sexp": nodeToSexp,
	}
	value.RegisterMethods(l, "treesitter.Node", methods)
}

// Navigation methods implementation

func nodeParent(l *lua.LState) int {
	node := checkNode(l)
	parent := node.node.Parent()
	if parent == nil {
		l.Push(lua.LNil)
		return 1
	}

	ud := l.NewUserData()
	ud.Value = &NodeWrapper{node: parent, source: node.source}
	ud.Metatable = value.GetTypeMetatable(l, "treesitter.Node")

	l.Push(ud)
	return 1
}

func nodeChild(l *lua.LState) int {
	node := checkNode(l)
	idx := uint(l.CheckNumber(2))
	child := node.node.Child(idx)
	if child == nil {
		l.Push(lua.LNil)
		return 1
	}
	ud := l.NewUserData()
	ud.Value = &NodeWrapper{node: child, source: node.source}
	ud.Metatable = value.GetTypeMetatable(l, "treesitter.Node")

	l.Push(ud)
	return 1
}

func nodeChildCount(l *lua.LState) int {
	node := checkNode(l)
	l.Push(lua.LNumber(node.node.ChildCount()))
	return 1
}

func nodeNextSibling(l *lua.LState) int {
	node := checkNode(l)
	sibling := node.node.NextSibling()
	if sibling == nil {
		l.Push(lua.LNil)
		return 1
	}
	ud := l.NewUserData()
	ud.Value = &NodeWrapper{node: sibling, source: node.source}
	ud.Metatable = value.GetTypeMetatable(l, "treesitter.Node")

	l.Push(ud)
	return 1
}

func nodePrevSibling(l *lua.LState) int {
	node := checkNode(l)
	sibling := node.node.PrevSibling()
	if sibling == nil {
		l.Push(lua.LNil)
		return 1
	}
	ud := l.NewUserData()
	// todo: eventually we need sync.map for that to avoid duplicate constructs,
	// todo: but this is optimization for later
	ud.Value = &NodeWrapper{node: sibling, source: node.source}
	ud.Metatable = value.GetTypeMetatable(l, "treesitter.Node")

	l.Push(ud)
	return 1
}

func nodeNextNamedSibling(l *lua.LState) int {
	node := checkNode(l)
	sibling := node.node.NextNamedSibling()
	if sibling == nil {
		l.Push(lua.LNil)
		return 1
	}
	ud := l.NewUserData()
	ud.Value = &NodeWrapper{node: sibling, source: node.source}
	ud.Metatable = value.GetTypeMetatable(l, "treesitter.Node")
	l.Push(ud)
	return 1
}

func nodePrevNamedSibling(l *lua.LState) int {
	node := checkNode(l)
	sibling := node.node.PrevNamedSibling()
	if sibling == nil {
		l.Push(lua.LNil)
		return 1
	}
	ud := l.NewUserData()
	ud.Value = &NodeWrapper{node: sibling, source: node.source}
	ud.Metatable = value.GetTypeMetatable(l, "treesitter.Node")
	l.Push(ud)
	return 1
}

func nodeNamedChild(l *lua.LState) int {
	node := checkNode(l)
	idx := uint(l.CheckNumber(2))
	child := node.node.NamedChild(idx)
	if child == nil {
		l.Push(lua.LNil)
		return 1
	}
	ud := l.NewUserData()
	ud.Value = &NodeWrapper{node: child, source: node.source}
	ud.Metatable = value.GetTypeMetatable(l, "treesitter.Node")
	l.Push(ud)
	return 1
}

func nodeNamedChildCount(l *lua.LState) int {
	node := checkNode(l)
	l.Push(lua.LNumber(node.node.NamedChildCount()))
	return 1
}

// Field-related methods implementation

func nodeChildByFieldName(l *lua.LState) int {
	node := checkNode(l)
	fieldName := l.CheckString(2)
	child := node.node.ChildByFieldName(fieldName)
	if child == nil {
		l.Push(lua.LNil)
		return 1
	}
	ud := l.NewUserData()
	ud.Value = &NodeWrapper{node: child, source: node.source}
	ud.Metatable = value.GetTypeMetatable(l, "treesitter.Node")
	l.Push(ud)
	return 1
}

func nodeFieldNameForChild(l *lua.LState) int {
	node := checkNode(l)
	idx := uint32(l.CheckNumber(2))
	fieldName := node.node.FieldNameForChild(idx)
	if fieldName == "" {
		l.Push(lua.LNil)
		return 1
	}
	l.Push(lua.LString(fieldName))
	return 1
}

// Inspection methods implementation

func nodeKind(l *lua.LState) int {
	node := checkNode(l)
	l.Push(lua.LString(node.node.Kind()))
	return 1
}

func nodeIsNamed(l *lua.LState) int {
	node := checkNode(l)
	l.Push(lua.LBool(node.node.IsNamed()))
	return 1
}

func nodeHasError(l *lua.LState) int {
	node := checkNode(l)
	l.Push(lua.LBool(node.node.HasError()))
	return 1
}

func nodeIsError(l *lua.LState) int {
	node := checkNode(l)
	l.Push(lua.LBool(node.node.IsError()))
	return 1
}

// Position methods implementation

func nodeStartByte(l *lua.LState) int {
	node := checkNode(l)
	l.Push(lua.LNumber(node.node.StartByte()))
	return 1
}

func nodeEndByte(l *lua.LState) int {
	node := checkNode(l)
	l.Push(lua.LNumber(node.node.EndByte()))
	return 1
}

func nodeStartPoint(l *lua.LState) int {
	node := checkNode(l)
	point := node.node.StartPosition()
	pointTable := l.CreateTable(0, 2)
	pointTable.RawSetString("row", lua.LNumber(point.Row))
	pointTable.RawSetString("column", lua.LNumber(point.Column))
	l.Push(pointTable)
	return 1
}

func nodeEndPoint(l *lua.LState) int {
	node := checkNode(l)
	point := node.node.EndPosition()
	pointTable := l.CreateTable(0, 2)
	pointTable.RawSetString("row", lua.LNumber(point.Row))
	pointTable.RawSetString("column", lua.LNumber(point.Column))
	l.Push(pointTable)
	return 1
}

// nodeText retrieves the source text for this node
func nodeText(l *lua.LState) int {
	node := checkNode(l)

	var code string
	if l.GetTop() == 2 && l.Get(2).Type() == lua.LTString {
		code = l.CheckString(2)
	} else {
		if node.source == nil {
			l.RaiseError("source reference is empty")
			return 0
		}

		code = *node.source
	}

	// Spawn byte positions
	// start and end can't be < 0
	start := node.node.StartByte()
	end := node.node.EndByte()

	sourceLen := len(code)
	startPos := start
	endPos := end

	if startPos > endPos || endPos > uint(sourceLen) {
		// Instead of just returning nil, raise an error
		l.RaiseError("invalid byte range")
		return 0 // This line won't be reached due to RaiseError
	}

	// Extract text
	text := code[start:end]
	l.Push(lua.LString(text))
	return 1
}

func nodeGrammarName(l *lua.LState) int {
	node := checkNode(l)
	grammarName := node.node.GrammarName()
	l.Push(lua.LString(grammarName))
	return 1
}

func nodeIsExtra(l *lua.LState) int {
	node := checkNode(l)
	l.Push(lua.LBool(node.node.IsExtra()))
	return 1
}

func nodeIsMissing(l *lua.LState) int {
	node := checkNode(l)
	l.Push(lua.LBool(node.node.IsMissing()))
	return 1
}

func nodeNamedDescendantForPointRange(l *lua.LState) int {
	node := checkNode(l)

	// Spawn start point table argument
	startPointTbl := l.CheckTable(2)
	startRow := uint(startPointTbl.RawGetString("row").(lua.LNumber))
	startCol := uint(startPointTbl.RawGetString("column").(lua.LNumber))

	// Spawn end point table argument
	endPointTbl := l.CheckTable(3)
	endRow := uint(endPointTbl.RawGetString("row").(lua.LNumber))
	endCol := uint(endPointTbl.RawGetString("column").(lua.LNumber))

	startPoint := treesitter.Point{Row: startRow, Column: startCol}
	endPoint := treesitter.Point{Row: endRow, Column: endCol}

	descendant := node.node.NamedDescendantForPointRange(startPoint, endPoint)
	if descendant == nil {
		l.Push(lua.LNil)
		return 1
	}

	ud := l.NewUserData()
	ud.Value = &NodeWrapper{node: descendant}
	ud.Metatable = value.GetTypeMetatable(l, "treesitter.Node")
	l.Push(ud)
	return 1
}

func nodeDescendantCount(l *lua.LState) int {
	node := checkNode(l)
	count := node.node.DescendantCount()
	l.Push(lua.LNumber(count))
	return 1
}

func nodeToSexp(l *lua.LState) int {
	node := checkNode(l)
	sexp := node.node.ToSexp()
	l.Push(lua.LString(sexp))
	return 1
}

// Helper functions

func checkNode(l *lua.LState) *NodeWrapper {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*NodeWrapper); ok {
		return v
	}

	l.ArgError(1, "Node expected")
	return nil
}
