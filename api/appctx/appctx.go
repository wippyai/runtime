package appctx

import (
	"sync"

	"github.com/ponyruntime/pony/api/event"
	fsapi "github.com/ponyruntime/pony/api/fs"
	funcapi "github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/payload"
	processapi "github.com/ponyruntime/pony/api/process"
	pubsubapi "github.com/ponyruntime/pony/api/pubsub"
	regapi "github.com/ponyruntime/pony/api/registry"
	resourceapi "github.com/ponyruntime/pony/api/resource"

	contractsys "github.com/ponyruntime/pony/system/contract"
	"github.com/ponyruntime/pony/system/env"
	"github.com/ponyruntime/pony/system/fs"
	"github.com/ponyruntime/pony/system/function"
	"github.com/ponyruntime/pony/system/interceptor"
	transcoder "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/process"
	"github.com/ponyruntime/pony/system/resource"
	"github.com/ponyruntime/pony/system/security"
	"github.com/ponyruntime/pony/system/topology"
	"go.uber.org/zap"
)

// AppCtx holds all application dependencies and provides thread-safe access to them.
// This replaces the previous approach of storing dependencies in context.Context,
// which was causing performance issues due to context bloat.
//
// IMPORTANT: AppCtx should be passed to functions that need dependencies,
// while context.Context should only be used for lifecycle management
// (cancel, timeout, deadline).
//
// AppCtx is created with all dependencies through the constructor:
//
//	appCtx := NewAppCtx(eventBus, security, fsRegistry, ...)
//
// Usage example:
//
//	appCtx := app.GetAppCtx()
//	eventBus := appCtx.GetEventBus()
//	security := appCtx.GetSecurity()
//
// Function signature example:
//
//	func ProcessData(ctx context.Context, appCtx *AppCtx, data []byte) error {
//		// ctx is used for cancellation/timeout
//		// appCtx is used for accessing dependencies
//		eventBus := appCtx.GetEventBus()
//		// ... process data
//	}
//
// All getter methods are thread-safe and use read locks for better performance.
// AppCtx is immutable after creation - all dependencies are set in the constructor.
type AppCtx struct {
	mu sync.RWMutex

	// Core services
	eventBus    event.Bus
	security    *security.PolicyRegistry
	fsRegistry  *fs.Registry
	envRegistry *env.Registry
	registry    regapi.Registry
	transcoder  *transcoder.Transcoder
	functions   *function.Registry
	processes   *process.Manager
	resources   *resource.Registry

	// PubSub and topology
	router      pubsubapi.Receiver
	node        pubsubapi.Node
	topology    *topology.Topology
	pidRegistry *topology.PIDRegistry

	// Logging and interceptors
	logger      *zap.Logger
	interceptor *interceptor.Registry

	// Contract system
	contractRegistry     *contractsys.Registry
	contractInstantiator *contractsys.Instantiator
}

// NewAppCtx creates a new AppCtx instance with all dependencies
func NewAppCtx(
	eventBus event.Bus,
	security *security.PolicyRegistry,
	fsRegistry *fs.Registry,
	envRegistry *env.Registry,
	registry regapi.Registry,
	transcoder *transcoder.Transcoder,
	functions *function.Registry,
	processes *process.Manager,
	resources *resource.Registry,
	router pubsubapi.Receiver,
	node pubsubapi.Node,
	topology *topology.Topology,
	pidRegistry *topology.PIDRegistry,
	logger *zap.Logger,
	interceptor *interceptor.Registry,
	contractRegistry *contractsys.Registry,
	contractInstantiator *contractsys.Instantiator,
) *AppCtx {
	return &AppCtx{
		eventBus:             eventBus,
		security:             security,
		fsRegistry:           fsRegistry,
		envRegistry:          envRegistry,
		registry:             registry,
		transcoder:           transcoder,
		functions:            functions,
		processes:            processes,
		resources:            resources,
		router:               router,
		node:                 node,
		topology:             topology,
		pidRegistry:          pidRegistry,
		logger:               logger,
		interceptor:          interceptor,
		contractRegistry:     contractRegistry,
		contractInstantiator: contractInstantiator,
	}
}

// GetEventBus returns the event bus
func (ac *AppCtx) GetEventBus() event.Bus {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.eventBus
}

// GetSecurity returns the security registry
func (ac *AppCtx) GetSecurity() *security.PolicyRegistry {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.security
}

// SetFSRegistry sets the filesystem registry
func (ac *AppCtx) SetFSRegistry(fsRegistry *fs.Registry) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.fsRegistry = fsRegistry
}

// GetFSRegistry returns the filesystem registry
func (ac *AppCtx) GetFSRegistry() fsapi.Registry {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.fsRegistry
}

// SetEnvRegistry sets the environment registry
func (ac *AppCtx) SetEnvRegistry(envRegistry *env.Registry) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.envRegistry = envRegistry
}

// GetEnvRegistry returns the environment registry
func (ac *AppCtx) GetEnvRegistry() *env.Registry {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.envRegistry
}

// SetRegistry sets the main registry
func (ac *AppCtx) SetRegistry(registry regapi.Registry) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.registry = registry
}

// GetRegistry returns the main registry
func (ac *AppCtx) GetRegistry() regapi.Registry {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.registry
}

// SetTranscoder sets the transcoder
func (ac *AppCtx) SetTranscoder(transcoder *transcoder.Transcoder) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.transcoder = transcoder
}

// GetTranscoder returns the transcoder
func (ac *AppCtx) GetTranscoder() payload.Transcoder {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.transcoder
}

// SetFunctions sets the function registry
func (ac *AppCtx) SetFunctions(functions *function.Registry) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.functions = functions
}

// GetFunctions returns the function registry
func (ac *AppCtx) GetFunctions() funcapi.Registry {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.functions
}

// SetProcesses sets the process manager
func (ac *AppCtx) SetProcesses(processes *process.Manager) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.processes = processes
}

// GetProcesses returns the process manager
func (ac *AppCtx) GetProcesses() processapi.Manager {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.processes
}

// SetResources sets the resource registry
func (ac *AppCtx) SetResources(resources *resource.Registry) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.resources = resources
}

// GetResources returns the resource registry
func (ac *AppCtx) GetResources() resourceapi.Registry {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.resources
}

// SetRouter sets the pubsub router
func (ac *AppCtx) SetRouter(router pubsubapi.Receiver) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.router = router
}

// GetRouter returns the pubsub router
func (ac *AppCtx) GetRouter() pubsubapi.Receiver {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.router
}

// SetNode sets the pubsub node
func (ac *AppCtx) SetNode(node pubsubapi.Node) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.node = node
}

// GetNode returns the pubsub node
func (ac *AppCtx) GetNode() pubsubapi.Node {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.node
}

// SetTopology sets the topology
func (ac *AppCtx) SetTopology(topology *topology.Topology) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.topology = topology
}

// GetTopology returns the topology
func (ac *AppCtx) GetTopology() *topology.Topology {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.topology
}

// SetPIDRegistry sets the PID registry
func (ac *AppCtx) SetPIDRegistry(pidRegistry *topology.PIDRegistry) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.pidRegistry = pidRegistry
}

// GetPIDRegistry returns the PID registry
func (ac *AppCtx) GetPIDRegistry() *topology.PIDRegistry {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.pidRegistry
}

// SetLogger sets the logger
func (ac *AppCtx) SetLogger(logger *zap.Logger) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.logger = logger
}

// GetLogger returns the logger
func (ac *AppCtx) GetLogger() *zap.Logger {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.logger
}

// SetInterceptor sets the interceptor registry
func (ac *AppCtx) SetInterceptor(interceptor *interceptor.Registry) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.interceptor = interceptor
}

// GetInterceptor returns the interceptor registry
func (ac *AppCtx) GetInterceptor() *interceptor.Registry {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.interceptor
}

// SetContractRegistry sets the contract registry
func (ac *AppCtx) SetContractRegistry(contractRegistry *contractsys.Registry) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.contractRegistry = contractRegistry
}

// GetContractRegistry returns the contract registry
func (ac *AppCtx) GetContractRegistry() *contractsys.Registry {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.contractRegistry
}

// SetContractInstantiator sets the contract instantiator
func (ac *AppCtx) SetContractInstantiator(contractInstantiator *contractsys.Instantiator) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.contractInstantiator = contractInstantiator
}

// GetContractInstantiator returns the contract instantiator
func (ac *AppCtx) GetContractInstantiator() *contractsys.Instantiator {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.contractInstantiator
}
