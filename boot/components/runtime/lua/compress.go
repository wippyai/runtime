//go:build plugin_lua_compress

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	bootpkg "github.com/wippyai/runtime/boot"
	compressmod "github.com/wippyai/runtime/runtime/lua/modules/compress"
)

func LuaCompress() boot.Component {
	return boot.New(boot.P{
		Name:      LuaCompressName,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, compressmod.NewCompressModule()); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
