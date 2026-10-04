// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/eval"
)

const EvalModuleName boot.Name = "lua.eval"

// EvalModule registers the eval Lua module, which compiles dynamic source and
// runs it as supervised processes through the node's eval host.
func EvalModule() boot.Component {
	return boot.New(boot.P{
		Name:      EvalModuleName,
		DependsOn: []boot.Name{EngineName, EvalHostName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}
			return ctx, AddModules(ctx, cm, eval.Module)
		},
	})
}
