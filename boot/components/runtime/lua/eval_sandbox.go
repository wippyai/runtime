package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/eval/sandbox"
)

const EvalSandboxName boot.Name = "lua.eval_sandbox"

func EvalSandbox() boot.Component {
	return boot.New(boot.P{
		Name:      EvalSandboxName,
		DependsOn: []boot.Name{EngineName, EvalHostName},
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
