package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/runtime/lua/modules/treesitter"
)

func TreeSitter() boot.Component {
	return boot.New(boot.P{
		Name:      LuaTreeSitterName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm,
				treesitter.NewTreeSitterModule(logger.Named("tsitter")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
