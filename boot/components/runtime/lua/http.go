// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	httpapimod "github.com/wippyai/runtime/runtime/lua/modules/http"
	"github.com/wippyai/runtime/runtime/lua/modules/httpclient"
)

func HTTP() boot.Component {
	return boot.New(boot.P{
		Name:      HTTPName,
		DependsOn: []boot.Name{EngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, httpapimod.Module, httpclient.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
