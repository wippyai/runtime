package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/eval/runner"
)

const LuaEvalRunnerName boot.Name = "lua.eval_runner"

func EvalRunner() boot.Component {
	return boot.New(boot.P{
		Name:      LuaEvalRunnerName,
		DependsOn: []boot.Name{LuaEngineName, EvalHostName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, runner.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
