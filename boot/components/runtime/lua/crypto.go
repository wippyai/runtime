package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/crypto"
	"github.com/wippyai/runtime/runtime/lua/modules/hash"
)

func Crypto() boot.Component {
	return boot.New(boot.P{
		Name:      LuaCryptoName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm,
				crypto.NewCryptoModule(),
				hash.NewHashModule(),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
