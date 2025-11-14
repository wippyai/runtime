package lua

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/registry"
	luaapi "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/code"
)

var codeManagerKey = &ctxapi.Key{Name: "lua.codeManager"}

// SetCodeManager stores the code manager in AppContext.
func SetCodeManager(ctx context.Context, cm *code.Manager) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(codeManagerKey) == nil {
		ac.With(codeManagerKey, cm)
	}
	return ctx
}

// GetCodeManager retrieves the code manager from AppContext.
func GetCodeManager(ctx context.Context) *code.Manager {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(codeManagerKey); val != nil {
		if cm, ok := val.(*code.Manager); ok {
			return cm
		}
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
