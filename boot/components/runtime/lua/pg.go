// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/pg"
)

func PG() boot.Component {
	return boot.New(boot.P{
		Name:      PGModuleName,
		DependsOn: []boot.Name{EngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, pg.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
