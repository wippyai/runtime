package activity

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	api "github.com/ponyruntime/pony/api/service/temporal"
	"log"
	"sync"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/service/temporal/client"
	"go.uber.org/zap"
)

// Manager creates and manages activity handlers
type Manager struct {
	mu       sync.RWMutex
	log      *zap.Logger
	executor runtime.Executor
	configs  map[registry.ID]*api.ActivityConfig
}

// NewActivityManager creates a new activity manager instance
func NewActivityManager(log *zap.Logger, executor runtime.Executor) *Manager {
	return &Manager{
		log:      log,
		executor: executor,
		configs:  make(map[registry.ID]*api.ActivityConfig),
	}
}

// Register creates an activity handler function
// The client is used only for context binding in the returned handler
func (m *Manager) Register(id registry.ID, cfg *api.ActivityConfig, client *client.Client) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.configs[id]; exists {
		return nil, fmt.Errorf("activity %s already registered", id)
	}

	// Store just the config
	m.configs[id] = cfg

	// Create handler function with client context binding
	handler := func(ctx context.Context, args payload.Payloads) (payload.Payloads, error) {
		m.log.Info("executing activity", zap.String("activity_id", string(id)))
		log.Printf("Activity received input: %v \n", ctx)
		// todo: merge contexts or move temporal specific one to our thread

		for _, a := range args {
			log.Printf("Activity received input: %v \n", a)
		}

		// TODO: Later we will:
		// 1. Create a runtime.Task from the activity input
		// 2. Execute through the executor
		// 3. Convert the result to temporal payloads
		// 4. Handle proper error mapping

		return nil, nil
	}

	/*

		// TODO: _____________________ YES
		w.RegisterActivityWithOptions(
			func(ctx context.Context, args *payload.Payload) (*commonpb.Payloads, error) {
				// todo: we simply can pass client over context actually
				actInfo := tmact.GetInfo(ctx)

				// todo: convert and send to executor actually
				// todo: do we actually want to perform transcode to wippy payloads?
				// todo: probably yes
				s.log.Info("stab activity executed")
				log.Printf("%+v %+v", args, actInfo)
				log.Printf("Activity result: %v\n", ctx)
				return nil, nil
			}, tmact.RegisterOptions{
				Name: "stab-activity",
			})
		log.Printf("Registered activity: stab-activity\n")
		// TODO: _____________________ YES

	*/
	m.log.Info("registered activity handler", zap.String("id", string(id)))

	return handler, nil
}

// Get retrieves an activity configuration
func (m *Manager) Get(id registry.ID) (*api.ActivityConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cfg, exists := m.configs[id]
	return cfg, exists
}

// Delete removes an activity configuration
func (m *Manager) Delete(id registry.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.configs[id]; !exists {
		return fmt.Errorf("activity %s not found", id)
	}

	delete(m.configs, id)
	m.log.Info("deleted activity configuration", zap.String("id", string(id)))
	return nil
}

// Has checks if an activity configuration exists
func (m *Manager) Has(id registry.ID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.configs[id]
	return exists
}
