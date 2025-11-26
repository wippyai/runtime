package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	workflowfuncs "github.com/wippyai/runtime/runtime/lua/modules/workflow/funcs"
	workflowos "github.com/wippyai/runtime/runtime/lua/modules/workflow/ostime"
	workflowprocess "github.com/wippyai/runtime/runtime/lua/modules/workflow/process"
	workflowtime "github.com/wippyai/runtime/runtime/lua/modules/workflow/time"
)

const WorkflowModulesName boot.ComponentName = "lua.workflow_modules"

func WorkflowModules() boot.Component {
	return boot.New(boot.P{
		Name:      WorkflowModulesName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm,
				workflowprocess.NewProcessModule(),
				workflowos.NewOSTimeModule(),
				workflowtime.NewTimeModule(),
				workflowfuncs.NewFuncsModule(),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
