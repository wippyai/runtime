package lua

import (
	"context"
	httpbase "net/http"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	luaapi "github.com/ponyruntime/pony/api/runtime/lua"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/runtime/lua/code"
	"github.com/ponyruntime/pony/runtime/lua/command"
	"github.com/ponyruntime/pony/runtime/lua/component"
	bteaapp "github.com/ponyruntime/pony/runtime/lua/component/btea"
	funclua "github.com/ponyruntime/pony/runtime/lua/component/function"
	"github.com/ponyruntime/pony/runtime/lua/component/library"
	proclua "github.com/ponyruntime/pony/runtime/lua/component/process"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	"github.com/ponyruntime/pony/runtime/lua/engine/upstream"
	"github.com/ponyruntime/pony/runtime/lua/modules/base64"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea"
	"github.com/ponyruntime/pony/runtime/lua/modules/cloudstorage"
	contractmod "github.com/ponyruntime/pony/runtime/lua/modules/contract"
	"github.com/ponyruntime/pony/runtime/lua/modules/crypto"
	ctxmod "github.com/ponyruntime/pony/runtime/lua/modules/ctx"
	envlua "github.com/ponyruntime/pony/runtime/lua/modules/env"
	"github.com/ponyruntime/pony/runtime/lua/modules/events"
	"github.com/ponyruntime/pony/runtime/lua/modules/excel"
	"github.com/ponyruntime/pony/runtime/lua/modules/exec"
	"github.com/ponyruntime/pony/runtime/lua/modules/expr"
	fsmod "github.com/ponyruntime/pony/runtime/lua/modules/fs"
	"github.com/ponyruntime/pony/runtime/lua/modules/funcmod"
	fncallmod "github.com/ponyruntime/pony/runtime/lua/modules/funcs"
	"github.com/ponyruntime/pony/runtime/lua/modules/hash"
	"github.com/ponyruntime/pony/runtime/lua/modules/html"
	httpapimod "github.com/ponyruntime/pony/runtime/lua/modules/http"
	"github.com/ponyruntime/pony/runtime/lua/modules/httpclient"
	jsonmod "github.com/ponyruntime/pony/runtime/lua/modules/json"
	loggermod "github.com/ponyruntime/pony/runtime/lua/modules/logger"
	"github.com/ponyruntime/pony/runtime/lua/modules/ostime"
	otelmod "github.com/ponyruntime/pony/runtime/lua/modules/otel"
	payloadmod "github.com/ponyruntime/pony/runtime/lua/modules/payload"
	processmod "github.com/ponyruntime/pony/runtime/lua/modules/process"
	processmodapi "github.com/ponyruntime/pony/runtime/lua/modules/processmod"
	registrymod "github.com/ponyruntime/pony/runtime/lua/modules/registry"
	securitymod "github.com/ponyruntime/pony/runtime/lua/modules/security"
	sqlmod "github.com/ponyruntime/pony/runtime/lua/modules/sql"
	"github.com/ponyruntime/pony/runtime/lua/modules/store"
	"github.com/ponyruntime/pony/runtime/lua/modules/system"
	luatemplate "github.com/ponyruntime/pony/runtime/lua/modules/template"
	"github.com/ponyruntime/pony/runtime/lua/modules/text"
	timemod "github.com/ponyruntime/pony/runtime/lua/modules/time"
	"github.com/ponyruntime/pony/runtime/lua/modules/treesitter"
	"github.com/ponyruntime/pony/runtime/lua/modules/uuid"
	"github.com/ponyruntime/pony/runtime/lua/modules/websocket"
	yamlmod "github.com/ponyruntime/pony/runtime/lua/modules/yaml"
	"github.com/ponyruntime/pony/runtime/lua/task"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
)

func Engine() boot.Component {
	return boot.New(boot.P{
		Name:      LuaEngineName,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)
			cfg := boot.GetConfig(ctx)

			// Get cache sizes from config with defaults
			protoCacheSize := 60000
			mainCacheSize := 10000
			if cfg != nil {
				luaCfg := cfg.Sub("lua")
				if luaCfg != nil {
					protoCacheSize = luaCfg.GetInt("proto_cache_size", protoCacheSize)
					mainCacheSize = luaCfg.GetInt("main_cache_size", mainCacheSize)
				}
			}

			codeManager, err := code.NewCodeManager(
				logger.Named("lua"),
				bus,
				code.Config{
					Modules: []luaapi.Module{
						envlua.NewEnvModule(),
						ostime.NewOSTimeModule(),
						channel.NewChannelModule(),
						timemod.NewTimeModule(),
						loggermod.NewLoggerModule(logger),
						base64.NewBase64Module(),
						jsonmod.NewJSONModule(),
						fsmod.NewFSModule(),
						uuid.NewUUIDModule(),
						upstream.NewUpstreamModule(),
						subscribe.NewSubscribeModule(),
						crypto.NewCryptoModule(),
						fncallmod.NewFunctionModule(),
						payloadmod.NewPayloadModule(),
						task.NewTaskModule(),
						hash.NewHashModule(),
						command.NewCommandModule(),
						yamlmod.NewYAMLModule(),
						text.NewTextModule(),
						registrymod.NewLoaderModule(logger.Named("loader")),
						events.NewEventsModule(logger.Named("events")),
						exec.NewExecModule(logger.Named("exec")),
						ctxmod.NewCtxModule(logger.Named("ctx")),
						store.NewStoreModule(logger.Named("store")),
						luatemplate.NewTemplateModule(logger.Named("template")),
						securitymod.NewSecurityModule(logger.Named("security")),
						registrymod.NewRegistryModule(logger.Named("registry")),
						processmod.NewProcessAPIModule(logger.Named("proc")),
						httpapimod.NewHTTPAPIModule(logger.Named("http")),
						processmodapi.NewProcessAPIModule(logger.Named("inbox")),
						funcmod.NewFunctionAPIModule(logger.Named("inbox")),
						httpclient.NewHTTPClientModule(logger.Named("http"), httpbase.DefaultClient),
						websocket.NewWebSocketModule(logger.Named("websocket")),
						treesitter.NewTreeSitterModule(logger.Named("tsitter")),
						btea.NewBteaModule(logger.Named("btea")),
						sqlmod.NewSQLModule(logger.Named("sql")),
						excel.NewModule(logger.Named("excel")),
						cloudstorage.NewModule(),
						system.NewSystemModule(),
						contractmod.NewContractModule(logger.Named("contract")),
						otelmod.NewOTelModule(),
						expr.NewExprModule(expr.WithCapacity(5000)),
						html.NewHTMLModule(),
					},
					ProtoCacheSize: 60000,
					MainCacheSize:  10000,
				},
			)
			if err != nil {
				return ctx, err
			}

			// Store code manager in context for other plugins to use
			ctx = SetCodeManager(ctx, codeManager)

			// Create component managers
			funcs := funclua.NewManager(logger.Named("lua.funcs"), codeManager, bus)
			libraries := library.NewManager(logger.Named("lua.libs"), codeManager)
			processes := proclua.NewProcessManager(logger.Named("lua.proc"), codeManager, bus)
			terminalApps := bteaapp.NewBteaManager(logger.Named("lua.bteaapp"), codeManager, bus)

			// Register all handlers
			handlers.Register(reghandler.NewTransactionHandler(codeManager))
			handlers.Register(component.NewHandler("function.lua", funcs))
			handlers.Register(component.NewHandler("library.lua", libraries))
			handlers.Register(component.NewHandler("process.lua", processes))
			handlers.Register(component.NewHandler("btea.app.lua", terminalApps))

			return ctx, nil
		},
	})
}
