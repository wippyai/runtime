package main

import (
	"context"
	"fmt"
	httpbase "net/http"
	"os"
	"time"

	"github.com/ponyruntime/pony/runtime/lua/modules/html"

	"github.com/ponyruntime/pony/api/registry"

	"github.com/go-chi/chi/v5/middleware"
	luaapi "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/moduleloader"
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
	"github.com/ponyruntime/pony/runtime/lua/modules/ctx"
	envlua "github.com/ponyruntime/pony/runtime/lua/modules/env"
	"github.com/ponyruntime/pony/runtime/lua/modules/events"
	"github.com/ponyruntime/pony/runtime/lua/modules/excel"
	"github.com/ponyruntime/pony/runtime/lua/modules/exec"
	fsmod "github.com/ponyruntime/pony/runtime/lua/modules/fs"
	"github.com/ponyruntime/pony/runtime/lua/modules/funcmod"
	fncallmod "github.com/ponyruntime/pony/runtime/lua/modules/funcs"
	"github.com/ponyruntime/pony/runtime/lua/modules/hash"
	httpapimod "github.com/ponyruntime/pony/runtime/lua/modules/http"
	"github.com/ponyruntime/pony/runtime/lua/modules/httpclient"
	jsonmod "github.com/ponyruntime/pony/runtime/lua/modules/json"
	"github.com/ponyruntime/pony/runtime/lua/modules/logger"
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
	"github.com/ponyruntime/pony/service/aws/config"
	"github.com/ponyruntime/pony/service/aws/s3"
	"github.com/ponyruntime/pony/service/di"
	fsdir "github.com/ponyruntime/pony/service/directory"
	envservice "github.com/ponyruntime/pony/service/env"
	native "github.com/ponyruntime/pony/service/exec"
	prochost "github.com/ponyruntime/pony/service/host"
	"github.com/ponyruntime/pony/service/http"
	"github.com/ponyruntime/pony/service/http/cors"
	"github.com/ponyruntime/pony/service/http/firewall"
	"github.com/ponyruntime/pony/service/http/websocketrelay"
	"github.com/ponyruntime/pony/service/memstore"
	"github.com/ponyruntime/pony/service/policy"
	"github.com/ponyruntime/pony/service/processfunc"
	"github.com/ponyruntime/pony/service/sql"
	"github.com/ponyruntime/pony/service/sqlstore"
	service "github.com/ponyruntime/pony/service/supervisor"
	"github.com/ponyruntime/pony/service/template"
	"github.com/ponyruntime/pony/service/terminal"
	"github.com/ponyruntime/pony/service/tokenstore"
	"github.com/ponyruntime/pony/system/eventbus"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
	"github.com/wippyai/module-registry-proto-go/registry/identity/v1/identityv1connect"
	"github.com/wippyai/module-registry-proto-go/registry/module/v1/modulev1connect"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otelresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	oteltrace "go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/zap"
)

// createServiceHandlers configures all service handlers for the application
func createServiceHandlers(a *App) eventbus.RouterOption {
	return eventbus.WithHandlers(append(
		withLuaRuntime(a),
		withYamlPolicies(a),
		withEnvManager(a),
		withDirectoryManager(a),
		withHTTPService(a),
		withTokenStoreManager(a),
		withTerminalManager(a),
		withProcessSupervisor(a),
		withEphemeralHost(a),
		withSQLManager(a),
		withSQLStore(a),
		withAWSConfigManager(a),
		withS3Manager(a),
		withProcessFunctionBridge(a),
		withMemStore(a),
		withNativeExecutor(a),
		withJetTemplates(a),
		withContractSystem(a),
	)...)
}

func withTokenStoreManager(a *App) eventbus.EventHandler {
	// Create token store manager
	manager := tokenstore.NewManager(
		a.eventBus,
		a.dtt,
		a.resources,
		a.security,
		a.logger.Named("tstore"),
	)

	// Register manager for token store related entries
	return reghandler.NewRegistryHandler("security.token_store", manager)
}

func withHTTPService(a *App) eventbus.EventHandler {
	// Create factories
	endpointFactory, err := http.NewEndpointFactory(a.funcs)
	if err != nil {
		panic(fmt.Errorf("failed to create endpoint factory: %w", err))
	}

	staticFactory, err := http.NewStaticFactory(a.fsRegistry)
	if err != nil {
		panic(fmt.Errorf("failed to create static factory: %w", err))
	}

	// Create websocket relay manager
	relayManager := websocketrelay.NewWebSocketRelay(a.ctx, a.logger.Named("ws"))

	// Create middleware factory with all standard middleware
	midFactory := http.NewDefaultMiddlewareFactory(
		http.WithLogger(a.logger.Named("http.md")),

		http.WithMiddlewareCreator(cors.MiddlewareName, cors.CreateCORSMiddleware),

		// Standard Chi middlewares
		http.WithMiddleware("recoverer", middleware.Recoverer),
		http.WithMiddleware("request_id", middleware.RequestID),
		http.WithMiddleware("real_ip", middleware.RealIP),

		// Timeout middleware with options
		http.WithMiddlewareCreator("timeout", func(options map[string]string) func(handler httpbase.Handler) httpbase.Handler {
			timeoutVal := options["timeout"]
			if timeoutVal == "" {
				timeoutVal = "60s"
			}
			duration, err := time.ParseDuration(timeoutVal)
			if err != nil {
				return nil
			}
			return middleware.Timeout(duration)
		}),

		// WebSocket relay middleware
		http.WithMiddleware("websocket_relay", relayManager.Middleware),
		http.WithMiddlewareCreator(tokenstore.MiddlewareName, tokenstore.CreateTokenAuthMiddleware),
		http.WithMiddlewareCreator(firewall.ResourceMiddlewareName, firewall.CreateResourceFirewallMiddleware),
		http.WithMiddlewareCreator(firewall.EndpointMiddlewareName, firewall.CreateEndpointFirewallMiddleware),
	)

	// Create manager with all required factories
	manager, err := http.NewManager(
		a.dtt,
		a.eventBus,
		http.NewServerFactory(midFactory),
		endpointFactory,
		staticFactory,
		a.logger.Named("http"),
	)
	if err != nil {
		panic(fmt.Errorf("failed to create http manager: %w", err))
	}

	return reghandler.NewRegistryHandler("http.*", manager)
}

func withTerminalManager(a *App) eventbus.EventHandler {
	return reghandler.NewRegistryHandler("terminal.host", terminal.NewTerminalManager(
		a.eventBus,
		a.dtt,
		a.logger.Named("terminal"),
	))
}

func withProcessSupervisor(a *App) eventbus.EventHandler {
	return reghandler.NewRegistryHandler("process.service", service.NewSupervisorServiceManager(
		a.eventBus,
		a.processes,
		a.logger.Named("super"),
	))
}

func withEphemeralHost(a *App) eventbus.EventHandler {
	return reghandler.NewRegistryHandler("process.host", prochost.NewHostManager(
		a.eventBus,
		a.dtt,
		a.logger.Named("hosts"),
	))
}

func withDirectoryManager(a *App) eventbus.EventHandler {
	return reghandler.NewRegistryHandler("fs.directory", fsdir.NewDirectoryManager(
		a.eventBus,
		a.dtt,
		nil,
		a.logger.Named("fs.dir"),
	))
}

func withAWSConfigManager(a *App) eventbus.EventHandler {
	return reghandler.NewRegistryHandler("config.aws", config.NewManager(
		a.eventBus,
		a.dtt,
		a.logger.Named("config.aws"),
		a.envRegistry,
	))
}

func withS3Manager(a *App) eventbus.EventHandler {
	return reghandler.NewRegistryHandler("cloudstorage.s3", s3.NewManager(
		a.eventBus,
		a.dtt,
		a.logger.Named("cloudstorage.s3"),
	))
}

func withEnvManager(a *App) eventbus.EventHandler {
	return reghandler.NewRegistryHandler("env.**", envservice.NewManager(
		a.eventBus,
		a.dtt,
		a.logger.Named("env"),
	))
}

func withSQLManager(a *App) eventbus.EventHandler {
	// Create manager with required dependencies
	manager, err := sql.NewManager(
		a.dtt,
		a.eventBus,
		a.logger.Named("sql"),
		a.envRegistry,
	)
	if err != nil {
		panic(fmt.Errorf("failed to create sql manager: %w", err))
	}

	// Register handler for all SQL-related kinds
	return reghandler.NewRegistryHandler("db.sql.*", manager)
}

func withYamlPolicies(a *App) eventbus.EventHandler {
	// Create manager with required dependencies
	manager := policy.NewManager(
		a.eventBus,
		policy.NewDefaultFactory(a.dtt),
		a.logger.Named("policy"),
	)

	// Register handler for all SQL-related kinds
	return reghandler.NewRegistryHandler("security.policy", manager)
}

func withMemStore(a *App) eventbus.EventHandler {
	// Create manager with required dependencies
	manager := memstore.NewManager(
		a.eventBus,
		a.dtt,
		a.logger.Named("memory"),
	)

	return reghandler.NewRegistryHandler("store.memory", manager)
}

func withSQLStore(a *App) eventbus.EventHandler {
	// Create manager with required dependencies
	manager := sqlstore.NewManager(
		a.eventBus,
		a.dtt,
		a.logger.Named("sqlstore"),
	)

	return reghandler.NewRegistryHandler("store.sql", manager)
}

func withNativeExecutor(a *App) eventbus.EventHandler {
	// Create manager with required dependencies
	manager := native.NewManager(
		a.eventBus,
		a.dtt,
		a.logger.Named("exec"),
	)

	return reghandler.NewRegistryHandler("exec.native", manager)
}

func withProcessFunctionBridge(a *App) eventbus.EventHandler {
	return processfunc.WithProcessFunctionBridge(
		a.logger.Named("pfunc"),
		a.eventBus,
		a.processes,
	)
}

func withJetTemplates(a *App) eventbus.EventHandler {
	// Create manager with required dependencies
	manager := template.NewManager(
		a.eventBus,
		a.dtt,
		a.logger.Named("tmpl"),
	)

	return reghandler.NewRegistryHandler("template.(jet|set)", manager)
}

func withContractSystem(a *App) eventbus.EventHandler {
	// Create manager for handling contract definitions and bindings
	manager := di.NewManager(
		a.eventBus,
		a.dtt,
		a.logger.Named("contract"),
	)

	// Register handler for contract definitions and bindings
	return reghandler.NewRegistryHandler("contract.(definition|binding)", manager)
}

func withLuaRuntime(a *App) []eventbus.EventHandler {
	codeManager, err := code.NewCodeManager(
		a.logger.Named("lua"),
		a.eventBus,
		code.Config{
			Modules: []luaapi.Module{
				envlua.NewEnvModule(),
				ostime.NewOSTimeModule(),
				channel.NewChannelModule(),
				timemod.NewTimeModule(),
				logger.NewLoggerModule(a.logger.Named("app")),
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
				registrymod.NewLoaderModule(a.logger.Named("loader")),
				events.NewEventsModule(a.logger.Named("events")),
				exec.NewExecModule(a.logger.Named("exec")),
				ctx.NewCtxModule(a.logger.Named("ctx")),
				store.NewStoreModule(a.logger.Named("store")),
				luatemplate.NewTemplateModule(a.logger.Named("template")),
				securitymod.NewSecurityModule(a.logger.Named("security")),
				registrymod.NewRegistryModule(a.logger.Named("registry")),
				processmod.NewProcessAPIModule(a.logger.Named("proc")),
				httpapimod.NewHTTPAPIModule(a.logger.Named("http")),
				processmodapi.NewProcessAPIModule(a.logger.Named("inbox")),
				funcmod.NewFunctionAPIModule(a.logger.Named("inbox")),
				httpclient.NewHTTPClientModule(a.logger.Named("http"), httpbase.DefaultClient),
				websocket.NewWebSocketModule(a.logger.Named("websocket")),
				treesitter.NewTreeSitterModule(a.logger.Named("tsitter")),
				btea.NewBteaModule(a.logger.Named("btea")),
				sqlmod.NewSQLModule(a.logger.Named("sql")),
				excel.NewModule(a.logger.Named("excel")),
				cloudstorage.NewModule(),
				system.NewSystemModule(),
				contractmod.NewContractModule(a.logger.Named("contract")),
				otelmod.NewOTelModule(),
				html.NewHTMLModule(),
			},
			ProtoCacheSize: 60000,
			MainCacheSize:  10000,
		},
	)
	if err != nil {
		panic(err)
	}

	funcs := funclua.NewManager(a.logger.Named("lua.funcs"), codeManager, a.eventBus)
	libraries := library.NewManager(a.logger.Named("lua.libs"), codeManager)
	processes := proclua.NewProcessManager(a.logger.Named("lua.proc"), codeManager, a.eventBus)
	terminalApps := bteaapp.NewBteaManager(a.logger.Named("lua.bteaapp"), codeManager, a.eventBus)

	return []eventbus.EventHandler{
		reghandler.NewTransactionHandler(codeManager),
		component.NewHandler("function.lua", funcs),
		component.NewHandler("library.lua", libraries),
		component.NewHandler("process.lua", processes),
		component.NewHandler("btea.app.lua", terminalApps),
	}
}

func newModuleloaderManager(baseURL string, entries []registry.Entry, logger *zap.Logger) *moduleloader.Manager {
	client := &httpbase.Client{}
	organizationClient := identityv1connect.NewOrganizationServiceClient(client, baseURL)
	moduleClient := modulev1connect.NewModuleServiceClient(client, baseURL)
	labelClient := modulev1connect.NewLabelServiceClient(client, baseURL)
	commitClient := modulev1connect.NewCommitServiceClient(client, baseURL)
	downloadClient := modulev1connect.NewDownloadServiceClient(client, baseURL)

	registryLoader := moduleloader.NewEntryLoader(entries, logger)

	return moduleloader.NewManager(
		organizationClient,
		moduleClient,
		commitClient,
		labelClient,
		downloadClient,
		registryLoader,
		moduleloader.VendorFolder,
	)
}

// initOpenTelemetry initializes the OpenTelemetry tracer
func initOpenTelemetry(
	ctx context.Context,
	endpoint string,
	serviceName string,
	serviceVersion string,
	mainLogger *zap.Logger,
) (func(), error) {
	if endpoint == "" {
		mainLogger.Info("No OpenTelemetry endpoint specified, using no-op tracer")
		otel.SetTracerProvider(oteltrace.NewTracerProvider())
		return func() {}, nil
	}

	if serviceName == "" {
		serviceName = "wippy-runtime"
	}
	if serviceVersion == "" {
		serviceVersion = "1.0.0"
	}

	// Create OTLP exporter
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithEndpoint(endpoint),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create resource with service information
	res, err := otelresource.New(ctx,
		otelresource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String(serviceVersion),
			semconv.HostNameKey.String(getHostname()),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create trace provider with sampling
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)

	// Store cleanup function
	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err = tp.Shutdown(cleanupCtx); err != nil {
			mainLogger.Warn("Error shutting down tracer provider", zap.Error(err))
		}
	}

	return cleanup, nil
}

// getHostname returns the hostname of the current machine
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}
