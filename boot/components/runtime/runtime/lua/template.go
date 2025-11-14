//go:build plugin_lua_template

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	logapi "github.com/ponyruntime/pony/api/logs"
	bootpkg "github.com/ponyruntime/pony/boot"
	luatemplate "github.com/ponyruntime/pony/runtime/lua/modules/template"
)

func LuaTemplate() boot.Component {
	return boot.New(boot.P{
		Name:      LuaTemplateName,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				luatemplate.NewTemplateModule(logger.Named("template")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
