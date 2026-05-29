// SPDX-License-Identifier: MPL-2.0

//go:build treesitter

package treesitter

import (
	treesitter "github.com/tree-sitter/go-tree-sitter"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const typeNode = "treesitter.Node"

// NodeWrapper wraps a tree-sitter Node for Lua integration
type NodeWrapper struct {
	node   *treesitter.Node
	source *string
}

// pushNode pushes a Node userdata to the stack
func pushNode(l *lua.LState, node *treesitter.Node, source *string) {
	value.PushTypedUserData(l, &NodeWrapper{node: node, source: source}, typeNode)
}

// Navigation methods implementation

func nodeParent(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	parent := node.node.Parent()
	if parent == nil {
		l.Push(lua.LNil)
		return 1
	}
	pushNode(l, parent, node.source)
	return 1
}

func nodeChild(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	idx := uint(l.CheckNumber(2))
	child := node.node.Child(idx)
	if child == nil {
		l.Push(lua.LNil)
		return 1
	}
	pushNode(l, child, node.source)
	return 1
}

func nodeChildCount(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	l.Push(lua.LNumber(node.node.ChildCount()))
	return 1
}

func nodeNextSibling(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	sibling := node.node.NextSibling()
	if sibling == nil {
		l.Push(lua.LNil)
		return 1
	}
	pushNode(l, sibling, node.source)
	return 1
}

func nodePrevSibling(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	sibling := node.node.PrevSibling()
	if sibling == nil {
		l.Push(lua.LNil)
		return 1
	}
	pushNode(l, sibling, node.source)
	return 1
}

func nodeNextNamedSibling(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	sibling := node.node.NextNamedSibling()
	if sibling == nil {
		l.Push(lua.LNil)
		return 1
	}
	pushNode(l, sibling, node.source)
	return 1
}

func nodePrevNamedSibling(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	sibling := node.node.PrevNamedSibling()
	if sibling == nil {
		l.Push(lua.LNil)
		return 1
	}
	pushNode(l, sibling, node.source)
	return 1
}

func nodeNamedChild(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	idx := uint(l.CheckNumber(2))
	child := node.node.NamedChild(idx)
	if child == nil {
		l.Push(lua.LNil)
		return 1
	}
	pushNode(l, child, node.source)
	return 1
}

func nodeNamedChildCount(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	l.Push(lua.LNumber(node.node.NamedChildCount()))
	return 1
}

// Field-related methods implementation

func nodeChildByFieldName(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	fieldName := l.CheckString(2)
	child := node.node.ChildByFieldName(fieldName)
	if child == nil {
		l.Push(lua.LNil)
		return 1
	}
	pushNode(l, child, node.source)
	return 1
}

func nodeFieldNameForChild(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
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
	if node == nil {
		return 0
	}
	l.Push(lua.LString(node.node.Kind()))
	return 1
}

func nodeIsNamed(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	l.Push(lua.LBool(node.node.IsNamed()))
	return 1
}

func nodeHasError(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	l.Push(lua.LBool(node.node.HasError()))
	return 1
}

func nodeIsError(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	l.Push(lua.LBool(node.node.IsError()))
	return 1
}

// Position methods implementation

func nodeStartByte(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	l.Push(lua.LNumber(node.node.StartByte()))
	return 1
}

func nodeEndByte(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	l.Push(lua.LNumber(node.node.EndByte()))
	return 1
}

func nodeStartPoint(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	point := node.node.StartPosition()
	pointTable := l.CreateTable(0, 2)
	pointTable.RawSetString("row", lua.LNumber(point.Row))
	pointTable.RawSetString("column", lua.LNumber(point.Column))
	l.Push(pointTable)
	return 1
}

func nodeEndPoint(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
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
	if node == nil {
		return 0
	}

	var code string
	if l.GetTop() == 2 && l.Get(2).Type() == lua.LTString {
		code = l.CheckString(2)
	} else {
		if node.source == nil {
			err := lua.NewLuaError(l, "source reference is empty").
				WithKind(lua.Invalid).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
			return 2
		}

		code = *node.source
	}

	start := node.node.StartByte()
	end := node.node.EndByte()

	sourceLen := len(code)

	if start > end || end > uint(sourceLen) {
		err := lua.NewLuaError(l, "invalid byte range").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	text := code[start:end]
	l.Push(lua.LString(text))
	return 1
}

func nodeGrammarName(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	grammarName := node.node.GrammarName()
	l.Push(lua.LString(grammarName))
	return 1
}

func nodeIsExtra(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	l.Push(lua.LBool(node.node.IsExtra()))
	return 1
}

func nodeIsMissing(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	l.Push(lua.LBool(node.node.IsMissing()))
	return 1
}

func nodeNamedDescendantForPointRange(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}

	// Spawn start point table argument
	startPointTbl := l.CheckTable(2)
	startRow := toUint(startPointTbl.RawGetString("row"))
	startCol := toUint(startPointTbl.RawGetString("column"))

	// Spawn end point table argument
	endPointTbl := l.CheckTable(3)
	endRow := toUint(endPointTbl.RawGetString("row"))
	endCol := toUint(endPointTbl.RawGetString("column"))

	startPoint := treesitter.Point{Row: startRow, Column: startCol}
	endPoint := treesitter.Point{Row: endRow, Column: endCol}

	descendant := node.node.NamedDescendantForPointRange(startPoint, endPoint)
	if descendant == nil {
		l.Push(lua.LNil)
		return 1
	}
	pushNode(l, descendant, node.source)
	return 1
}

func nodeDescendantCount(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
	count := node.node.DescendantCount()
	l.Push(lua.LNumber(count))
	return 1
}

func nodeToSexp(l *lua.LState) int {
	node := checkNode(l)
	if node == nil {
		return 0
	}
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
