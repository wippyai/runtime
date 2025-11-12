package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/registry"
	luaapi "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/code"
)

type ctxKey string

const codeManagerKey ctxKey = "lua.codeManager"

// SetCodeManager stores the code manager in the context.
func SetCodeManager(ctx context.Context, cm *code.Manager) context.Context {
	return context.WithValue(ctx, codeManagerKey, cm)
}

// GetCodeManager retrieves the code manager from the context.
func GetCodeManager(ctx context.Context) *code.Manager {
	if cm, ok := ctx.Value(codeManagerKey).(*code.Manager); ok {
		return cm
	}
	return nil
}

// AddModules adds modules to the code manager by creating module nodes.
func AddModules(ctx context.Context, cm *code.Manager, modules ...luaapi.Module) error {
	for _, mod := range modules {
		node := code.Node{
			ID:     registry.ID{NS: "", Name: mod.Name()},
			Kind:   luaapi.KindModule,
			Module: mod,
		}
		if err := cm.AddNode(ctx, node, nil); err != nil {
			return err
		}
	}
	return nil
}
