package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/eval/sandbox"
)

const LuaEvalSandboxName boot.Name = "lua.eval_sandbox"

func EvalSandbox() boot.Component {
	return boot.New(boot.P{
		Name:      LuaEvalSandboxName,
		DependsOn: []boot.Name{LuaEngineName, EvalHostName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, sandbox.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
