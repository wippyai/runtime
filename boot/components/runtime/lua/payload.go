package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	payloadmod "github.com/wippyai/runtime/runtime/lua/modules/payload"
)

func Payload() boot.Component {
	return boot.New(boot.P{
		Name:      LuaPayloadName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm,
				payloadmod.NewPayloadModule(),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
