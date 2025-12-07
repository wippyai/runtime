package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	cloudstorage "github.com/wippyai/runtime/runtime/lua/modules/cloudstorage"
)

func CloudStorage() boot.Component {
	return boot.New(boot.P{
		Name:      LuaCloudStorageName,
		DependsOn: []boot.Name{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, cloudstorage.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
