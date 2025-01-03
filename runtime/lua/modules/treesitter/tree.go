package treesitter

import (
	treesitter "github.com/tree-sitter/go-tree-sitter"
	"github.com/yuin/gopher-lua"
)

// TreeWrapper wraps a tree-sitter Tree for Lua integration
type TreeWrapper struct {
	tree *treesitter.Tree
}

// Register the Tree type to Lua
func registerTree(L *lua.LState) {
	mt := L.NewTypeMetatable("treesitter.Tree")
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), treeMethods))
	L.SetField(mt, "__gc", L.NewFunction(treeGC))
}

var treeMethods = map[string]lua.LGFunction{
	"root_node": treeRootNode,
	"copy":      treeCopy,
	"walk":      treeWalk,
}

// Tree methods implementation

func treeRootNode(L *lua.LState) int {
	tree := checkTree(L)
	if tree.tree == nil {
		L.Push(lua.LNil)
		return 1
	}

	root := tree.tree.RootNode()
	if root == nil {
		L.Push(lua.LNil)
		return 1
	}

	// Create and push new Node userdata
	ud := L.NewUserData()
	ud.Value = &NodeWrapper{node: root}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Node"))
	L.Push(ud)
	return 1
}

func treeCopy(L *lua.LState) int {
	tree := checkTree(L)
	if tree.tree == nil {
		L.Push(lua.LNil)
		return 1
	}

	copied := tree.tree.Clone()
	if copied == nil {
		L.Push(lua.LNil)
		return 1
	}

	ud := L.NewUserData()
	ud.Value = &TreeWrapper{tree: copied}
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Tree"))
	L.Push(ud)
	return 1
}

func treeWalk(L *lua.LState) int {
	tree := checkTree(L)
	if tree.tree == nil {
		L.Push(lua.LNil)
		return 1
	}

	cursor := tree.tree.Walk()
	if cursor == nil {
		L.Push(lua.LNil)
		return 1
	}

	ud := L.NewUserData()
	ud.Value = cursor
	L.SetMetatable(ud, L.GetTypeMetatable("treesitter.Cursor"))
	L.Push(ud)
	return 1
}

func treeGC(L *lua.LState) int {
	tree := checkTree(L)
	if tree.tree != nil {
		tree.tree.Close()
	}
	return 0
}

// Helper functions

func checkTree(L *lua.LState) *TreeWrapper {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*TreeWrapper); ok {
		return v
	}
	L.ArgError(1, "Tree expected")
	return nil
}
