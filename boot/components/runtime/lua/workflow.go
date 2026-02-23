// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	workflowmod "github.com/wippyai/runtime/runtime/lua/modules/workflow"
)

func Workflow() boot.Component {
	return boot.New(boot.P{
		Name:      WorkflowName,
		DependsOn: []boot.Name{EngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, workflowmod.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
