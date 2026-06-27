// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	archivemod "github.com/wippyai/runtime/runtime/lua/modules/archive"
)

func Archive() boot.Component {
	return boot.New(boot.P{
		Name:      ArchiveName,
		DependsOn: []boot.Name{EngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, archivemod.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
