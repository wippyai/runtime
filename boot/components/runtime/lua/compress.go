package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	compressmod "github.com/wippyai/runtime/runtime/lua/modules/compress"
)

func Compress() boot.Component {
	return boot.New(boot.P{
		Name:      CompressName,
		DependsOn: []boot.Name{EngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, compressmod.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
