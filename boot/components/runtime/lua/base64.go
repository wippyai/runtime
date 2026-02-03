package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/base64"
)

func Base64() boot.Component {
	return boot.New(boot.P{
		Name:      Base64Name,
		DependsOn: []boot.Name{EngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, base64.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
