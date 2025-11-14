//go:build plugin_lua_text

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/runtime/lua/modules/text"
)

func LuaText() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaText,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, text.NewTextModule()); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
