package main

import (
	"fmt"
	"sync"
)

// AppCtx represents the application context with all dependencies
// This is a simplified version for demonstration purposes
//
//nolint:unused // demonstration struct with unused fields for example purposes
type AppCtx struct {
	mu sync.RWMutex

	// Core services
	eventBus    string // event.Bus
	security    string // *security.PolicyRegistry
	fsRegistry  string // *fs.Registry
	envRegistry string // *env.Registry
	registry    string // regapi.Registry
	transcoder  string // *transcoder.Transcoder
	functions   string // *function.Registry
	processes   string // *process.Manager
	resources   string // *resource.Registry

	// PubSub and topology
	router      string // pubsubapi.Receiver
	node        string // pubsubapi.Node
	topology    string // *topology.Topology
	pidRegistry string // *topology.PIDRegistry

	// Logging and interceptors
	logger      string // *zap.Logger
	interceptor string // *interceptor.Registry

	// Contract system
	contractRegistry     string // *contractsys.Registry
	contractInstantiator string // *contractsys.Instantiator
}

// NewAppCtx creates a new AppCtx instance
func NewAppCtx() *AppCtx {
	return &AppCtx{}
}

// Example getter methods (simplified)
func (ac *AppCtx) GetEventBus() string {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.eventBus
}

func (ac *AppCtx) GetSecurity() string {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.security
}

func (ac *AppCtx) GetFSRegistry() string {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.fsRegistry
}

func (ac *AppCtx) GetRegistry() string {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.registry
}

func (ac *AppCtx) GetRouter() string {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.router
}

func (ac *AppCtx) GetNode() string {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.node
}

// Example setter methods (simplified)
func (ac *AppCtx) SetEventBus(bus string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.eventBus = bus
}

func (ac *AppCtx) SetSecurity(security string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.security = security
}

func (ac *AppCtx) SetFSRegistry(fsRegistry string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.fsRegistry = fsRegistry
}

func (ac *AppCtx) SetRegistry(registry string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.registry = registry
}

func (ac *AppCtx) SetRouter(router string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.router = router
}

func (ac *AppCtx) SetNode(node string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.node = node
}

// ExampleAppCtxUsage demonstrates how to use the new AppCtx architecture
func ExampleAppCtxUsage() {
	// Create a new AppCtx instance
	appCtx := NewAppCtx()

	// Set some dependencies
	appCtx.SetEventBus("eventBus")
	appCtx.SetSecurity("security")
	appCtx.SetFSRegistry("fsRegistry")
	appCtx.SetRegistry("registry")
	appCtx.SetRouter("router")
	appCtx.SetNode("node")

	// Access dependencies through AppCtx
	eventBus := appCtx.GetEventBus()
	if eventBus != "" {
		fmt.Printf("✓ EventBus available: %v\n", eventBus)
	}

	security := appCtx.GetSecurity()
	if security != "" {
		fmt.Printf("✓ Security registry available: %v\n", security)
	}

	fsRegistry := appCtx.GetFSRegistry()
	if fsRegistry != "" {
		fmt.Printf("✓ Filesystem registry available: %v\n", fsRegistry)
	}

	// Example of thread-safe access
	go func() {
		// Concurrent access to AppCtx
		registry := appCtx.GetRegistry()
		if registry != "" {
			fmt.Printf("✓ Registry accessed from goroutine: %v\n", registry)
		}
	}()

	// Example of setting dependencies
	appCtx.SetEventBus("newEventBus")
	appCtx.SetSecurity("newSecurity")

	fmt.Println("AppCtx is ready for use!")
}

// ExampleDependencyAccess shows how to access dependencies in different parts of your code
func ExampleDependencyAccess(appCtx *AppCtx) {
	// Access core services
	eventBus := appCtx.GetEventBus()
	security := appCtx.GetSecurity()
	fsRegistry := appCtx.GetFSRegistry()

	// Access PubSub and topology
	router := appCtx.GetRouter()
	node := appCtx.GetNode()

	// Use the dependencies
	if eventBus != "" {
		fmt.Printf("EventBus is available: %v\n", eventBus)
	}

	if security != "" {
		fmt.Printf("Security registry is available: %v\n", security)
	}

	if fsRegistry != "" {
		fmt.Printf("Filesystem registry is available: %v\n", fsRegistry)
	}

	// Example: Use the router for message sending
	if router != "" {
		fmt.Printf("Router is available: %v\n", router)
	}

	// Example: Use the node for host registration
	if node != "" {
		fmt.Printf("Node is available: %v\n", node)
	}
}

func main() {
	fmt.Println("AppCtx Usage Examples")
	fmt.Println("======================")

	// Run the example
	ExampleAppCtxUsage()

	// Create another instance for dependency access example
	appCtx := NewAppCtx()
	appCtx.SetEventBus("exampleEventBus")
	appCtx.SetSecurity("exampleSecurity")
	appCtx.SetRouter("exampleRouter")
	appCtx.SetNode("exampleNode")

	fmt.Println("\n--- Dependency Access Example ---")
	ExampleDependencyAccess(appCtx)

	fmt.Println("\nNote: This is a demonstration of the AppCtx pattern.")
	fmt.Println("In a real application, you would access AppCtx from your App instance")
	fmt.Println("after calling App.Start().")
}
