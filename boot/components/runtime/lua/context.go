package lua

import (
	"context"

	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
)

// SetCodeManager stores the code manager in AppContext.
func SetCodeManager(ctx context.Context, cm *code.Manager) context.Context {
	return luaapi.SetCodeManager(ctx, cm)
}

// GetCodeManager retrieves the code manager from AppContext.
func GetCodeManager(ctx context.Context) *code.Manager {
	if cm := luaapi.GetCodeManager(ctx); cm != nil {
		if typed, ok := cm.(*code.Manager); ok {
			return typed
		}
	}
	return nil
}

// AddModules adds modules to the code manager by creating module nodes.
// Also registers module types with the type checker for built-in availability.
func AddModules(ctx context.Context, cm *code.Manager, modules ...*luaapi.ModuleDef) error {
	for _, mod := range modules {
		node := code.Node{
			ID:     registry.NewID("", mod.Name),
			Kind:   luaapi.ModuleKind,
			Module: mod,
		}
		if mod.Types != nil {
			node.Manifest = mod.Types()
		}
		if err := cm.AddNode(ctx, node, nil); err != nil {
			return err
		}
		cm.AddBuiltinType(mod)
	}
	return nil
}
