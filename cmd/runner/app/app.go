package app

import (
	"context"
	"errors"
	"fmt"
	iofs "io/fs"
	httpbase "net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/ponyruntime/pony/api/cluster"
	"github.com/ponyruntime/pony/api/contract"
	envapi "github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/event"
	fsapi "github.com/ponyruntime/pony/api/fs"
	funcapi "github.com/ponyruntime/pony/api/function"
	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	procapi "github.com/ponyruntime/pony/api/process"
	pubsubapi "github.com/ponyruntime/pony/api/pubsub"
	regapi "github.com/ponyruntime/pony/api/registry"
	resourceapi "github.com/ponyruntime/pony/api/resource"
	secapi "github.com/ponyruntime/pony/api/security"
	topapi "github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/cluster/internode"
	"github.com/ponyruntime/pony/cluster/membership"
	"github.com/ponyruntime/pony/deps"
	requirementresolver2 "github.com/ponyruntime/pony/deps/requirementresolver"
	"github.com/ponyruntime/pony/embed"
	"github.com/ponyruntime/pony/internal/runtimeconfig"
	contractsys "github.com/ponyruntime/pony/system/contract"
	"github.com/ponyruntime/pony/system/env"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/ponyruntime/pony/system/fs"
	"github.com/ponyruntime/pony/system/function"
	"github.com/ponyruntime/pony/system/interceptor"
	"github.com/ponyruntime/pony/system/logs"
	transcoder "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/lua"
	"github.com/ponyruntime/pony/system/payload/yaml"
	"github.com/ponyruntime/pony/system/process"
	"github.com/ponyruntime/pony/system/pubsub"
	"github.com/ponyruntime/pony/system/registry"
	"github.com/ponyruntime/pony/system/registry/history"
	"github.com/ponyruntime/pony/system/registry/loader"
	"github.com/ponyruntime/pony/system/registry/loader/interpolate"
	"github.com/ponyruntime/pony/system/registry/runner"
	regtop "github.com/ponyruntime/pony/system/registry/topology"
	"github.com/ponyruntime/pony/system/resource"
	"github.com/ponyruntime/pony/system/security"
	"github.com/ponyruntime/pony/system/supervisor"
	"github.com/ponyruntime/pony/system/topology"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	_ "github.com/go-sql-driver/mysql" // MySQL driver for database connections
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

type appConfig struct {
	folderPath     string
	lockFilePath   string
	modulesDirPath string
	lockFileDir    string
	useEmbed       bool

	consoleLogging bool
	eventStreaming bool
	minLevel       zapcore.Level

	enableProfiling bool

	clusterEnabled    bool
	clusterName       string
	clusterBind       string
	clusterPort       int
	clusterJoin       string
	clusterSecret     string
	clusterSecretFile string
	clusterAdvertise  string

	runtimeConfig *runtimeconfig.Config

	// Directories to exclude from source scanning
	excludeDirs []string
}

// filteredFS wraps an fs.FS and filters out multiple directories.
// It pre-cleans all exclude paths for efficient comparison.
type filteredFS struct {
	fs                 iofs.FS
	excludeDirs        []string
	cleanedExcludeDirs []string // Pre-cleaned paths for performance
}

// Open implements fs.FS
func (f *filteredFS) Open(name string) (iofs.File, error) {
	if f.shouldExclude(name) {
		return nil, iofs.ErrNotExist
	}
	return f.fs.Open(name)
}

// ReadDir implements fs.ReadDirFS
func (f *filteredFS) ReadDir(name string) ([]iofs.DirEntry, error) {
	if f.shouldExclude(name) {
		return nil, iofs.ErrNotExist
	}

	entries, err := iofs.ReadDir(f.fs, name)
	if err != nil {
		return nil, err
	}

	// Filter out the excluded directories from the results
	filtered := make([]iofs.DirEntry, 0, len(entries))
	for _, entry := range entries {
		entryPath := filepath.Join(name, entry.Name())
		if !f.shouldExclude(entryPath) {
			filtered = append(filtered, entry)
		}
	}

	return filtered, nil
}

// newFilteredFS creates a new filteredFS with pre-cleaned exclude paths for performance.
func newFilteredFS(fs iofs.FS, excludeDirs []string) *filteredFS {
	cleanedDirs := make([]string, 0, len(excludeDirs))
	for _, dir := range excludeDirs {
		if dir != "" {
			cleanedDirs = append(cleanedDirs, filepath.Clean(dir))
		}
	}
	return &filteredFS{
		fs:                 fs,
		excludeDirs:        excludeDirs,
		cleanedExcludeDirs: cleanedDirs,
	}
}

// shouldExclude checks if a path should be excluded.
// It uses pre-cleaned paths for efficient O(n) comparison where n is the number of exclude directories.
func (f *filteredFS) shouldExclude(name string) bool {
	if len(f.cleanedExcludeDirs) == 0 {
		return false
	}

	// Clean the path once for comparison
	cleanName := filepath.Clean(name)

	// Check against each pre-cleaned excluded directory
	for _, cleanExclude := range f.cleanedExcludeDirs {
		// Check if the path is exactly the exclude directory
		if cleanName == cleanExclude {
			return true
		}

		// Check if the path is inside the exclude directory
		rel, err := filepath.Rel(cleanExclude, cleanName)
		if err == nil && !strings.HasPrefix(rel, "..") {
			return true
		}
	}

	return false
}

type Option func(*appConfig)

func WithCluster(enabled bool, name, bind string, port int, join, secret, secretFile, advertise string) Option {
	return func(c *appConfig) {
		c.clusterEnabled = enabled
		c.clusterName = name
		c.clusterBind = bind
		c.clusterPort = port
		c.clusterJoin = join
		c.clusterSecret = secret
		c.clusterSecretFile = secretFile
		c.clusterAdvertise = advertise
	}
}

func WithPaths(folderPath, lockFilePath, modulesDirPath, lockFileDir string, useEmbed bool) Option {
	return func(c *appConfig) {
		c.folderPath = folderPath
		c.lockFilePath = lockFilePath
		c.modulesDirPath = modulesDirPath
		c.lockFileDir = lockFileDir
		c.useEmbed = useEmbed
	}
}

func WithLogging(consoleLogging, eventStreaming bool, minLevel zapcore.Level) Option {
	return func(c *appConfig) {
		c.consoleLogging = consoleLogging
		c.eventStreaming = eventStreaming
		c.minLevel = minLevel
	}
}

func WithProfiling(enabled bool) Option {
	return func(c *appConfig) {
		c.enableProfiling = enabled
	}
}

func WithRuntimeConfig(cfg *runtimeconfig.Config) Option {
	return func(c *appConfig) {
		c.runtimeConfig = cfg
	}
}

func WithExcludeDirs(excludeDirs []string) Option {
	return func(c *appConfig) {
		c.excludeDirs = excludeDirs
	}
}

type App struct {
	ctx         context.Context
	cancel      context.CancelFunc
	config      appConfig
	logger      *zap.Logger
	logCore     logapi.Core
	logManager  *logs.Manager
	eventBus    event.Bus
	eventRouter *eventbus.EventRouter
	security    *security.PolicyRegistry
	services    eventbus.RouterOption
	dtt         *transcoder.Transcoder
	reg         regapi.Registry
	supervisor  *supervisor.Supervisor
	funcs       *function.Registry
	processes   *process.Manager
	prototypes  *process.PrototypeRegistry
	hosts       *process.HostRegistry
	fsRegistry  *fs.Registry
	envRegistry *env.Registry
	resources   *resource.Registry
	interceptor *interceptor.Registry

	contractRegistry     *contractsys.Registry
	contractInstantiator *contractsys.Instantiator

	node       *pubsub.NodeManager
	router     pubsubapi.Receiver
	topo       *topology.Topology
	pidReg     *topology.PIDRegistry
	membership *membership.Service

	internodeService *internode.Service
	connManager      internode.ConnectionManager
	messageCodec     *internode.MessageCodec

	shuttingDown  bool
	forceShutdown chan struct{}
	otelCleanup   func()
}

func NewApp(logger *zap.Logger, opts ...Option) (*App, error) {
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(1 * 1024 * 1024 * 1024)
	}

	config := appConfig{
		consoleLogging: true,
		eventStreaming: false,
		minLevel:       zapcore.InfoLevel,
	}
	for _, opt := range opts {
		opt(&config)
	}

	appCtx, cancel := context.WithCancel(context.Background())

	bus := eventbus.NewBus()

	core := logs.NewCore(logger.Core(), bus)
	appLogger := zap.New(core)

	dtt := transcoder.GlobalTranscoder()
	json.Register(dtt)
	yaml.Register(dtt)
	lua.Register(dtt)

	app := &App{
		ctx:           appCtx,
		cancel:        cancel,
		config:        config,
		logger:        appLogger,
		logCore:       core,
		eventBus:      bus,
		services:      nil,
		dtt:           dtt,
		forceShutdown: make(chan struct{}),
	}

	return app, nil
}

func (a *App) Initialize() error {
	logConfig := logapi.Config{
		PropagateDownstream: a.config.consoleLogging,
		StreamToEvents:      a.config.eventStreaming,
		MinLevel:            a.config.minLevel,
	}

	logManager := logs.NewManager(a.eventBus, a.logCore, a.logger.Named("logs"), logConfig)
	a.logManager = logManager

	if err := a.logManager.Start(a.ctx); err != nil {
		return fmt.Errorf("failed to start log manager: %w", err)
	}

	if a.config.clusterEnabled {
		if err := a.initClusterMesh(); err != nil {
			return fmt.Errorf("failed to initialize cluster mesh: %w", err)
		}
	} else {
		a.initSingleNodeMesh()
	}

	a.security = security.NewPolicyRegistry(a.eventBus, a.logger.Named("security"))
	if err := a.security.Start(a.ctx); err != nil {
		return fmt.Errorf("failed to start security manager: %w", err)
	}

	a.reg = registry.NewRegistry(
		history.NewMemory(),
		runner.NewBusRunner(a.eventBus, a.logger.Named("runner")),
		regtop.NewStateBuilder(a.logger),
		a.logger.Named("registry"),
	)

	a.supervisor = supervisor.NewSupervisor(a.eventBus, a.logger.Named("core"))

	err := a.node.Node().RegisterHost(topapi.ControlHost, pubsub.NewHost(a.ctx, pubsub.HostConfig{
		BufferSize:  1024,
		WorkerCount: 16,
		Logger:      a.logger.Named("control"),
	}))
	if err != nil {
		return fmt.Errorf("failed to register control host: %w", err)
	}

	funcHost := pubsub.NewHost(a.ctx, pubsub.HostConfig{
		BufferSize:  1024,
		WorkerCount: 16,
		Logger:      a.logger.Named("functions"),
	})
	err = a.node.Node().RegisterHost(funcapi.HostID, funcHost)
	if err != nil {
		return fmt.Errorf("failed to register function host: %w", err)
	}

	a.fsRegistry = fs.NewFSRegistry(a.eventBus, a.logger.Named("fs"))

	a.funcs = function.NewFunctionRegistry(a.eventBus, funcHost, a.logger.Named("funcs"))
	a.prototypes = process.NewPrototypeFactory(a.eventBus, a.logger.Named("prototypes"))
	a.hosts = process.NewHostRegistry(a.eventBus, a.logger.Named("hosts"))
	a.resources = resource.NewResourceRegistry(a.eventBus, a.logger.Named("resources"))

	a.envRegistry = env.NewRegistry(a.eventBus, a.logger.Named("env"))

	a.interceptor = interceptor.NewInterceptorRegistry(a.eventBus, a.logger.Named("interceptor"))

	a.processes = process.NewProcessManager(
		a.hosts,
		a.prototypes,
		a.node.Node().ID(),
		a.logger.Named("processes"),
	)

	a.contractRegistry = contractsys.NewContractRegistry(a.eventBus, a.logger.Named("contracts"))
	a.contractInstantiator = contractsys.NewContractInstantiator(a.contractRegistry, a.funcs)

	return nil
}

func (a *App) initSingleNodeMesh() {
	nodeName := a.config.clusterName
	if nodeName == "" {
		if hostname, err := os.Hostname(); err == nil {
			nodeName = hostname
		} else {
			nodeName = "local"
		}
	}

	localNode := pubsub.NewNode(nodeName)

	a.node = pubsub.NewNodeManager(
		localNode,
		a.eventBus,
		a.logger.Named("pubsub"),
	)
	a.router = pubsub.NewRouter(localNode, nil)

	a.topo = topology.NewTopology(a.node.Node())
	a.pidReg = topology.NewPIDRegistry(topology.PIDRegistryConfig{
		Parent: nil,
		Logger: a.logger.Named("pid"),
	})
}

func (a *App) initClusterMesh() error {
	var joinAddrs []string
	if a.config.clusterJoin != "" {
		for _, addr := range strings.Split(a.config.clusterJoin, ",") {
			joinAddrs = append(joinAddrs, strings.TrimSpace(addr))
		}
	}

	a.messageCodec = internode.NewMessageCodec(a.dtt)

	connManagerConfig := internode.DefaultManagerConfig()
	connManagerConfig.LocalNodeID = a.config.clusterName
	connManagerConfig.BindAddr = "0.0.0.0"
	connManagerConfig.AutoPort = true
	connManagerConfig.Logger = a.logger

	a.connManager = internode.NewConnectionManager(connManagerConfig)

	tempCtx, tempCancel := context.WithCancel(context.Background())
	dummyCallback := func(_ cluster.NodeID, _ []byte) {}

	if err := a.connManager.Start(tempCtx, dummyCallback); err != nil {
		tempCancel()
		return fmt.Errorf("failed to pre-start connection manager for port allocation: %w", err)
	}

	actualPort := a.connManager.GetListenPort()
	a.logger.Info("Pre-allocated internode port", zap.Int("port", actualPort))

	if err := a.connManager.Stop(); err != nil {
		tempCancel()
		return fmt.Errorf("failed to stop connection manager after port allocation: %w", err)
	}
	tempCancel()

	nodeMeta := cluster.NodeMeta{
		"version":        "1.0.0",
		"role":           "wippy",
		"internode_port": strconv.Itoa(actualPort),
	}

	memberConfig := membership.Config{
		NodeName:     a.config.clusterName,
		BindAddr:     a.config.clusterBind,
		BindPort:     a.config.clusterPort,
		JoinAddrs:    joinAddrs,
		SecretFile:   a.config.clusterSecretFile,
		SecretString: a.config.clusterSecret,
		AdvertiseIP:  a.config.clusterAdvertise,
		VeryVerbose:  false,
		Meta:         nodeMeta,
	}

	a.membership = membership.NewService(memberConfig, a.eventBus, a.logger.Named("cluster"))

	localNode := pubsub.NewNode(a.config.clusterName)

	pkgCallback := func(pkg *pubsubapi.Package) error {
		return localNode.Send(pkg)
	}

	a.internodeService = internode.NewService(
		a.logger,
		a.connManager,
		a.messageCodec,
		pkgCallback,
		a.eventBus,
		a.membership,
	)

	a.router = pubsub.NewRouter(localNode, a.internodeService)

	a.node = pubsub.NewNodeManager(
		localNode,
		a.eventBus,
		a.logger.Named("pubsub"),
	)

	a.topo = topology.NewTopology(a.node.Node())
	a.pidReg = topology.NewPIDRegistry(topology.PIDRegistryConfig{
		Parent: nil,
		Logger: a.logger.Named("pid"),
	})

	a.logger.Info("cluster mesh with internode initialized",
		zap.String("node_name", a.config.clusterName),
		zap.Int("internode_port", actualPort),
		zap.Strings("join_addresses", joinAddrs),
		zap.Bool("encryption_enabled", a.config.clusterSecretFile != "" || a.config.clusterSecret != ""))

	return nil
}

func (a *App) Start() error {
	appCtx := a.ctx
	appCtx = event.WithBus(appCtx, a.eventBus)
	appCtx = secapi.WithRegistry(appCtx, a.security)
	appCtx = fsapi.WithFSRegistry(appCtx, a.fsRegistry)
	appCtx = envapi.WithRegistry(appCtx, a.envRegistry)
	appCtx = regapi.WithRegistry(appCtx, a.reg)
	appCtx = payload.WithTranscoder(appCtx, a.dtt)
	appCtx = funcapi.WithFunctions(appCtx, a.funcs)
	appCtx = procapi.WithProcesses(appCtx, a.processes)
	appCtx = resourceapi.WithResources(appCtx, a.resources)
	appCtx = pubsubapi.WithRouter(appCtx, a.router)
	appCtx = pubsubapi.WithNode(appCtx, a.node.Node())
	appCtx = topapi.WithTopology(appCtx, a.topo)
	appCtx = topapi.WithPIDRegistry(appCtx, a.pidReg)
	appCtx = logapi.WithLogger(appCtx, a.logger)
	appCtx = apiinterceptor.WithInterceptor(appCtx, a.interceptor)
	appCtx = contract.WithServices(appCtx, a.contractRegistry, a.contractInstantiator)

	router, err := eventbus.StartRouter(appCtx, a.eventBus, a.services)
	if err != nil {
		a.cancel()
		return fmt.Errorf("failed to create event router: %w", err)
	}
	a.eventRouter = router

	if err := a.fsRegistry.Start(appCtx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start filesystem service: %w", err)
	}

	if err := a.envRegistry.Start(appCtx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start env registry: %w", err)
	}

	if err := a.resources.Start(appCtx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start resource service: %w", err)
	}

	if err := a.interceptor.Start(appCtx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start interceptor service: %w", err)
	}

	if err := a.funcs.Start(appCtx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start function executor: %w", err)
	}

	if err := a.prototypes.Start(appCtx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start prototype registry: %w", err)
	}

	if err := a.hosts.Start(appCtx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start host registry: %w", err)
	}

	if err := a.contractRegistry.Start(appCtx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start contract registry: %w", err)
	}

	if a.config.clusterEnabled {
		if a.membership != nil {
			if err := a.membership.Start(appCtx); err != nil {
				a.cancel()
				return fmt.Errorf("failed to start membership service: %w", err)
			}
		}

		if a.internodeService != nil {
			if err := a.internodeService.Start(appCtx); err != nil {
				a.cancel()
				return fmt.Errorf("failed to start internode service: %w", err)
			}
		}
	}

	if err := a.node.Start(appCtx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start node mesh: %w", err)
	}

	if err := a.supervisor.Start(appCtx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start supervisor: %w", err)
	}

	var fSys iofs.FS
	if a.config.useEmbed {
		fSys, err = iofs.Sub(embed.FS(), a.config.folderPath)
		if err != nil {
			a.cancel()
			return fmt.Errorf("open embedded sub-filesystem (use . to open from root): %w", err)
		}
	} else {
		osRoot, err := os.OpenRoot(a.config.folderPath)
		if err != nil {
			a.cancel()
			return fmt.Errorf("open folder %s: %w", a.config.folderPath, err)
		}
		fSys = osRoot.FS()
	}

	// Apply filtering if we have directories to exclude
	if len(a.config.excludeDirs) > 0 {
		fSys = newFilteredFS(fSys, a.config.excludeDirs)
	}

	bootCtx, cancel := context.WithTimeout(appCtx, 300*time.Second)
	defer cancel()

	manager := interceptor.NewManager(a.eventBus, a.logger.Named("interceptor"))
	err = manager.InitInterceptors(appCtx)
	if err != nil {
		a.cancel()
		return fmt.Errorf("failed to initialize interceptors: %w", err)
	}

	appState, cleanup, err := a.loadApplicationState(bootCtx, fSys)
	if err != nil {
		a.cancel()
		return fmt.Errorf("load application state: %w", err)
	}

	a.otelCleanup = cleanup

	if _, err := a.reg.Apply(bootCtx, appState); err != nil {
		return fmt.Errorf("failed to apply initial state: %w", err)
	}

	return nil
}

func (a *App) loadApplicationState(ctx context.Context, appFS iofs.FS) (regapi.ChangeSet, func(), error) {
	folderLoader := loader.NewLoader(a.dtt, a.logger, interpolate.NewEntryInterpolator(a.dtt,
		interpolate.WithInterpolator(interpolate.LoadFile),
	))

	cleanup, err := initOpenTelemetry(
		context.Background(),
		os.Getenv("OTEL_ENDPOINT"), // todo: cleanup to env layer
		os.Getenv("OTEL_SERVICE_NAME"),
		os.Getenv("OTEL_SERVICE_VERSION"),
		a.logger,
	)
	if err != nil {
		a.logger.Error("failed to initialize OpenTelemetry", zap.Error(err))
	}

	entries, err := folderLoader.LoadFS(ctx, appFS)
	if err != nil {
		return nil, nil, fmt.Errorf("load entries: %w", err)
	}

	baseURL := "https://modules.wippy.ai"
	if modulesURL := os.Getenv("WIPPY_MODULES_URL"); modulesURL != "" {
		baseURL = modulesURL
	}

	registryLoader := newModuleloaderManager(baseURL, entries, a.logger.Named("registry-loader"))

	var loadResult *deps.LoadResult

	lockPath, err := deps.FindLockFile(a.config.folderPath, a.config.lockFilePath)
	if err != nil {
		return nil, nil, err
	}

	if lockPath != "" {
		a.logger.Info("using lock file", zap.String("lock_file", lockPath))
	}

	if lockPath != "" {
		a.logger.Info("loading modules using lock file", zap.String("lock_file", lockPath))
		lockFile, loadErr := deps.LoadLockFile(lockPath)
		if loadErr != nil {
			a.logger.Error("load lock file", zap.Error(loadErr))
		} else {
			loadResult = deps.ConvertFromLockFile(lockFile, lockPath)
		}
	} else {
		a.logger.Info("loading modules from registry")
		loadResult, err = registryLoader.Load(ctx)
		if err != nil {
			a.logger.Error("load modules from registry", zap.Error(err))
		}
	}

	if loadResult != nil && len(loadResult.Modules) > 0 {
		a.logger.Debug("loaded modules",
			zap.Int("count", len(loadResult.Modules)),
			zap.Any("modules", loadResult.Modules))

		projectRootFS, err := createProjectRootFS(a.config.folderPath)
		if err != nil {
			return nil, nil, fmt.Errorf("create project root filesystem: %w", err)
		}

		// Create mapping from module names to their parent dependency entry IDs
		parentDependencyMap := deps.CreateParentDependencyMap(entries, loadResult, a.logger)

		// Validate that there are no conflicts in parent dependency assignments
		if err := deps.ValidateParentDependencyConflicts(parentDependencyMap, a.logger); err != nil {
			return nil, nil, fmt.Errorf("parent dependency conflicts detected: %w", err)
		}

		dependencyEntries, err := loadEntriesFromLoadedModules(ctx, folderLoader, loadResult, projectRootFS, a.logger, parentDependencyMap)
		if err != nil {
			return nil, nil, fmt.Errorf("load dependencies: %w", err)
		}
		entries = append(entries, dependencyEntries...)
	}

	resolver := requirementresolver2.NewResolver(a.logger.Named("requirement-resolver"))

	entries, err = resolver.ResolveModuleDefinitions(entries)
	if err != nil {
		return nil, nil, err
	}

	// apply runtime configuration overrides
	if a.config.runtimeConfig != nil {
		entries, err = a.applyRuntimeConfigOverrides(entries)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to apply runtime config overrides: %w", err)
		}
	}

	boot, err := regtop.NewStateBuilder(a.logger).BuildDelta(regapi.State{}, entries)
	if err != nil {
		return nil, nil, fmt.Errorf("build state delta: %w", err)
	}

	return boot, cleanup, nil
}

func (a *App) Stop() error {
	a.shuttingDown = true

	cancelCtx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()

	go func() {
		select {
		case <-a.forceShutdown:
			a.logger.Warn("force shutdown triggered, canceling context")
			a.cancel()
		case <-cancelCtx.Done():
		}
	}()

	if a.otelCleanup != nil {
		a.otelCleanup()
	}

	if err := a.eventRouter.Stop(); err != nil {
		a.logger.Error("failed to stop router", zap.Error(err))
	}

	if err := a.supervisor.Stop(); err != nil {
		a.logger.Error("failed to stop supervisor", zap.Error(err))
	}

	if err := a.node.Stop(); err != nil {
		a.logger.Error("failed to stop node", zap.Error(err))
	}

	if a.config.clusterEnabled {
		if a.internodeService != nil {
			if err := a.internodeService.Stop(); err != nil {
				a.logger.Error("failed to stop internode service", zap.Error(err))
			}
		}

		if a.membership != nil {
			if err := a.membership.Stop(); err != nil {
				a.logger.Error("failed to stop membership service", zap.Error(err))
			}
		}
	}

	if err := a.contractRegistry.Stop(); err != nil {
		a.logger.Error("failed to stop contract registry", zap.Error(err))
	}

	if err := a.hosts.Stop(); err != nil {
		a.logger.Error("failed to stop hosts registry", zap.Error(err))
	}

	if err := a.interceptor.Stop(); err != nil {
		a.logger.Error("failed to stop interceptor service", zap.Error(err))
	}

	if err := a.prototypes.Stop(); err != nil {
		a.logger.Error("failed to stop prototype registry", zap.Error(err))
	}

	if err := a.funcs.Stop(); err != nil {
		a.logger.Error("failed to stop function executor", zap.Error(err))
	}

	if err := a.resources.Stop(); err != nil {
		a.logger.Error("failed to stop resource service", zap.Error(err))
	}

	if err := a.fsRegistry.Stop(); err != nil {
		a.logger.Error("failed to stop filesystem registry", zap.Error(err))
	}

	if err := a.envRegistry.Stop(); err != nil {
		a.logger.Error("failed to stop env registry", zap.Error(err))
	}

	if err := a.security.Stop(); err != nil {
		a.logger.Error("failed to stop security manager", zap.Error(err))
	}

	if err := a.logManager.Stop(); err != nil {
		a.logger.Error("failed to stop log manager", zap.Error(err))
	}

	a.cancel()
	_ = a.logger.Sync()

	return nil
}

func (a *App) StartProfiler() {
	if !a.config.enableProfiling {
		return
	}

	go func() {
		profilerAddr := "localhost:6060"
		a.logger.Info("starting pprof server", zap.String("address", profilerAddr))

		mux := httpbase.NewServeMux()
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

		server := &httpbase.Server{
			Addr:         profilerAddr,
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		}
		if err := server.ListenAndServe(); err != nil {
			if !errors.Is(err, httpbase.ErrServerClosed) {
				a.logger.Error("pprof server failed", zap.Error(err))
			}
		}
	}()
}

func (a *App) SetServices(services eventbus.RouterOption) {
	a.services = services
}

func (a *App) Shutdown() error {
	return a.Stop()
}

func (a *App) ForceShutdown() {
	select {
	case <-a.forceShutdown:
	default:
		close(a.forceShutdown)
	}
}

func createProjectRootFS(_ string) (iofs.FS, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get current directory: %w", err)
	}
	projectRoot := currentDir

	osRoot, err := os.OpenRoot(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("open project root %s: %w", projectRoot, err)
	}

	return osRoot.FS(), nil
}

func resolveModulePath(modulePath string, mainLogger *zap.Logger) (string, error) {
	switch {
	case strings.HasPrefix(modulePath, "/"):
		currentDir, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current working directory: %w", err)
		}

		if strings.HasPrefix(modulePath, currentDir) {
			relativePath := strings.TrimPrefix(modulePath, currentDir)
			resolvedPath := strings.TrimPrefix(relativePath, "/")
			resolvedPath = filepath.ToSlash(resolvedPath)

			mainLogger.Debug("resolved absolute replacement path",
				zap.String("originalPath", modulePath),
				zap.String("currentDir", currentDir),
				zap.String("relativePath", relativePath),
				zap.String("finalModulePath", resolvedPath))

			return resolvedPath, nil
		}
		return "", fmt.Errorf("replacement path %s is outside the current working directory %s", modulePath, currentDir)
	case strings.HasPrefix(modulePath, "./") || strings.HasPrefix(modulePath, "../"):
		cleanPath := strings.TrimPrefix(modulePath, "./")
		cleanPath = strings.TrimPrefix(cleanPath, "../")
		resolvedPath := filepath.ToSlash(cleanPath)

		mainLogger.Debug("resolved relative replacement path",
			zap.String("originalPath", modulePath),
			zap.String("cleanPath", cleanPath),
			zap.String("finalModulePath", resolvedPath))

		return resolvedPath, nil
	default:
		return filepath.ToSlash(modulePath), nil
	}
}

func loadEntriesFromLoadedModules(
	ctx context.Context,
	folderLoader *loader.Loader,
	loadResult *deps.LoadResult,
	rootFS iofs.FS,
	mainLogger *zap.Logger,
	parentDependencyMap map[string][]deps.ParentDependencyInfo, // Maps module name (vendor/name) to parent dependency entries with parameters
) ([]regapi.Entry, error) {
	if loadResult == nil || len(loadResult.Modules) == 0 {
		return nil, nil
	}

	var allEntries []regapi.Entry

	for _, module := range loadResult.Modules {
		modulePath, err := resolveModulePath(module.Path, mainLogger)
		if err != nil {
			return nil, err
		}

		moduleFS, err := iofs.Sub(rootFS, modulePath)
		if err != nil {
			return nil, fmt.Errorf("create sub-filesystem for module %s: %w", module.Path, err)
		}

		moduleEntries, err := folderLoader.LoadFS(ctx, moduleFS)
		if err != nil {
			return nil, fmt.Errorf("load entries from module %s: %w", module.Path, err)
		}

		// Set meta.parent for ns.requirement entries
		moduleName := module.Name.String() // Format: "vendor/name"
		if parentDependencies, exists := parentDependencyMap[moduleName]; exists {
			for i := range moduleEntries {
				if moduleEntries[i].Kind == regapi.KindNamespaceRequirement {
					// Find the best parent dependency based on parameter matching
					bestParentID := deps.SelectBestParentDependency(moduleEntries[i], parentDependencies, mainLogger)
					if bestParentID != "" {
						if moduleEntries[i].Meta == nil {
							moduleEntries[i].Meta = make(regapi.Metadata)
						}
						moduleEntries[i].Meta["parent"] = bestParentID
						mainLogger.Debug("set meta.parent for ns.requirement",
							zap.String("requirement_id", moduleEntries[i].ID.String()),
							zap.String("parent_dependency_id", bestParentID),
							zap.String("module_name", moduleName))
					}
				}
			}
		}

		allEntries = append(allEntries, moduleEntries...)
	}

	return allEntries, nil
}

// applyRuntimeConfigOverrides applies runtime configuration overrides to registry entries.
// Format: namespace:entry:field=value
func (a *App) applyRuntimeConfigOverrides(entries []regapi.Entry) ([]regapi.Entry, error) {
	if a.config.runtimeConfig == nil {
		return entries, nil
	}

	namespaces := a.config.runtimeConfig.GetAllNamespaces()
	if len(namespaces) == 0 {
		return entries, nil
	}

	modifiedCount := 0
	var errs []error
	for i := range entries {
		entry := &entries[i]

		for _, ns := range namespaces {
			if entry.ID.NS != ns {
				continue
			}

			nsConfig, exists := a.config.runtimeConfig.GetNamespace(ns)
			if !exists {
				continue
			}

			for entryName, entryConfig := range nsConfig {
				if entry.ID.Name != entryName {
					continue
				}

				if err := a.applyEntryRuntimeConfig(entry, entryConfig); err != nil {
					errs = append(errs, fmt.Errorf("entry %s: %w", entry.ID.String(), err))
					continue
				}

				modifiedCount++
				break
			}
		}
	}

	if len(errs) > 0 {
		return entries, errors.Join(errs...)
	}

	a.logger.Debug("Applied runtime configuration overrides",
		zap.Int("namespaces", len(namespaces)),
		zap.Int("entries_modified", modifiedCount))

	return entries, nil
}

func (a *App) applyEntryRuntimeConfig(entry *regapi.Entry, entryConfig runtimeconfig.EntryConfig) error {
	return a.applyNestedRuntimeConfigSingle(entry, entryConfig, "")
}

func (a *App) applyNestedRuntimeConfigSingle(targetEntry *regapi.Entry, config runtimeconfig.EntryConfig, prefix string) error {
	for key, value := range config {
		fieldPath := key
		if prefix != "" {
			fieldPath = prefix + "." + key
		}

		if err := a.applyFieldPathToEntry(targetEntry, fieldPath, value); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) applyFieldPathToEntry(targetEntry *regapi.Entry, fieldPath string, value string) error {
	valueStr := value

	jqQuery := ".data"
	if fieldPath != "" {
		switch {
		case strings.HasPrefix(fieldPath, "."):
			jqQuery = fieldPath
		case strings.HasPrefix(fieldPath, "meta."):
			jqQuery = "." + fieldPath
		case strings.HasPrefix(fieldPath, "data."):
			jqQuery = "." + fieldPath
		default:
			jqQuery = ".data." + fieldPath
		}
	}

	entryCopy := *targetEntry
	entriesSlice := []regapi.Entry{entryCopy}

	err := requirementresolver2.ApplyPathValueToEntriesWithGojq(jqQuery, valueStr, entriesSlice)
	if err != nil {
		return err
	}

	*targetEntry = entriesSlice[0]
	return nil
}
