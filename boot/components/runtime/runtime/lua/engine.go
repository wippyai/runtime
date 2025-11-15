package lua

import (
	"context"
	httpbase "net/http"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/command"
	"github.com/wippyai/runtime/runtime/lua/component"
	bteaapp "github.com/wippyai/runtime/runtime/lua/component/btea"
	funclua "github.com/wippyai/runtime/runtime/lua/component/function"
	"github.com/wippyai/runtime/runtime/lua/component/library"
	proclua "github.com/wippyai/runtime/runtime/lua/component/process"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	"github.com/wippyai/runtime/runtime/lua/engine/subscribe"
	"github.com/wippyai/runtime/runtime/lua/engine/upstream"
	"github.com/wippyai/runtime/runtime/lua/modules/base64"
	"github.com/wippyai/runtime/runtime/lua/modules/btea"
	"github.com/wippyai/runtime/runtime/lua/modules/cloudstorage"
	contractmod "github.com/wippyai/runtime/runtime/lua/modules/contract"
	"github.com/wippyai/runtime/runtime/lua/modules/crypto"
	ctxmod "github.com/wippyai/runtime/runtime/lua/modules/ctx"
	envlua "github.com/wippyai/runtime/runtime/lua/modules/env"
	"github.com/wippyai/runtime/runtime/lua/modules/events"
	"github.com/wippyai/runtime/runtime/lua/modules/excel"
	"github.com/wippyai/runtime/runtime/lua/modules/exec"
	"github.com/wippyai/runtime/runtime/lua/modules/expr"
	fsmod "github.com/wippyai/runtime/runtime/lua/modules/fs"
	"github.com/wippyai/runtime/runtime/lua/modules/funcmod"
	fncallmod "github.com/wippyai/runtime/runtime/lua/modules/funcs"
	"github.com/wippyai/runtime/runtime/lua/modules/hash"
	"github.com/wippyai/runtime/runtime/lua/modules/html"
	httpapimod "github.com/wippyai/runtime/runtime/lua/modules/http"
	"github.com/wippyai/runtime/runtime/lua/modules/httpclient"
	jsonmod "github.com/wippyai/runtime/runtime/lua/modules/json"
	loggermod "github.com/wippyai/runtime/runtime/lua/modules/logger"
	"github.com/wippyai/runtime/runtime/lua/modules/ostime"
	otelmod "github.com/wippyai/runtime/runtime/lua/modules/otel"
	payloadmod "github.com/wippyai/runtime/runtime/lua/modules/payload"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	processmodapi "github.com/wippyai/runtime/runtime/lua/modules/processmod"
	registrymod "github.com/wippyai/runtime/runtime/lua/modules/registry"
	securitymod "github.com/wippyai/runtime/runtime/lua/modules/security"
	sqlmod "github.com/wippyai/runtime/runtime/lua/modules/sql"
	"github.com/wippyai/runtime/runtime/lua/modules/store"
	"github.com/wippyai/runtime/runtime/lua/modules/system"
	luatemplate "github.com/wippyai/runtime/runtime/lua/modules/template"
	"github.com/wippyai/runtime/runtime/lua/modules/text"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	"github.com/wippyai/runtime/runtime/lua/modules/treesitter"
	"github.com/wippyai/runtime/runtime/lua/modules/uuid"
	"github.com/wippyai/runtime/runtime/lua/modules/websocket"
	yamlmod "github.com/wippyai/runtime/runtime/lua/modules/yaml"
	"github.com/wippyai/runtime/runtime/lua/task"
	reghandler "github.com/wippyai/runtime/system/registry/events"
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
			exprCapacity := 5000
			if cfg != nil {
				luaCfg := cfg.Sub("lua")
				if luaCfg != nil {
					protoCacheSize = luaCfg.GetInt("proto_cache_size", protoCacheSize)
					mainCacheSize = luaCfg.GetInt("main_cache_size", mainCacheSize)
					exprCapacity = luaCfg.GetInt("expr_capacity", exprCapacity)
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
						expr.NewExprModule(expr.WithCapacity(exprCapacity)),
						html.NewHTMLModule(),
					},
					ProtoCacheSize: protoCacheSize,
					MainCacheSize:  mainCacheSize,
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
