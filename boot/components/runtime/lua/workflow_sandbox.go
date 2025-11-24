package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	sandbox "github.com/wippyai/runtime/runtime/lua/modules/workflow_sandbox"
)

const WorkflowSandboxName boot.ComponentName = "lua.workflow_sandbox"

func WorkflowSandbox() boot.Component {
	return boot.New(boot.P{
		Name:      WorkflowSandboxName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm,
				sandbox.NewWorkflowSandboxModule(),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
