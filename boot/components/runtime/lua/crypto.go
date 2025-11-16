//go:build plugin_lua_crypto

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/crypto"
	"github.com/wippyai/runtime/runtime/lua/modules/hash"
)

func LuaCrypto() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaCrypto,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				crypto.NewCryptoModule(),
				hash.NewHashModule(),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
