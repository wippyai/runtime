//go:build plugin_lua_crypto

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/runtime/lua/modules/crypto"
	"github.com/ponyruntime/pony/runtime/lua/modules/hash"
)

func LuaCrypto() boot.Plugin {
	return boot.New(boot.P{
		Name:      bootpkg.LuaCrypto,
		Phase:     boot.PostInit,
		DependsOn: []string{LuaEngineName},
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
