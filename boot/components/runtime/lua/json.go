// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	jsonmod "github.com/wippyai/runtime/runtime/lua/modules/json"
)

func JSON() boot.Component {
	return boot.New(boot.P{
		Name:      JSONName,
		DependsOn: []boot.Name{EngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, jsonmod.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
