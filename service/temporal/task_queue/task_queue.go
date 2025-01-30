package task_queue

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/service/temporal"
	"github.com/ponyruntime/pony/service/temporal/client"
	tmact "go.temporal.io/sdk/activity"
	tmwfl "go.temporal.io/sdk/workflow"
	"sync"

	"go.temporal.io/sdk/worker"
	"go.uber.org/zap"
)

// TaskQueue implements supervisor.Service interface for Temporal task queue workers
type TaskQueue struct {
	mu     sync.RWMutex
	ctx    context.Context
	log    *zap.Logger
	id     registry.ID
	config *api.TaskQueueConfig
	client *client.Client
	worker worker.Worker

	// Internal registries
	workflows  map[string]interface{}
	activities map[string]interface{}

	// Status channel for supervisor
	statusChan chan any
	exit       chan struct{}
}

// NewTaskQueue creates a new task queue service instance
func NewTaskQueue(logger *zap.Logger, id registry.ID, config *api.TaskQueueConfig, client *client.Client) *TaskQueue {
	return &TaskQueue{
		log:        logger,
		id:         id,
		config:     config,
		client:     client,
		workflows:  make(map[string]interface{}),
		activities: make(map[string]interface{}),
	}
}

func (s *TaskQueue) ID() registry.ID {
	return s.id
}

// constructWorker creates a new worker with all registered workflows and activities
func (s *TaskQueue) constructWorker(ctx context.Context) (worker.Worker, error) {
	c, err := s.client.GetClient()
	if err != nil {
		s.log.Error("failed to get client", zap.Error(err))
		return nil, err
	}

	w := worker.New(c, s.config.TaskQueue, s.config.ToWorkerOptions())

	// Mount all registered workflows
	for name, workflow := range s.workflows {
		w.RegisterWorkflowWithOptions(workflow, tmwfl.RegisterOptions{Name: name})
	}

	// Mount all registered activities
	for name, activity := range s.activities {
		w.RegisterActivityWithOptions(activity, tmact.RegisterOptions{Name: name})
	}

	return w, nil
}

// Start implements supervisor.Service interface
func (s *TaskQueue) Start(ctx context.Context) (<-chan any, error) {
	s.mu.Lock()
	if s.worker != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("worker already started")
	}

	s.ctx = ctx

	// Create and mount worker with all registered components
	w, err := s.constructWorker(ctx)
	if err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("failed to construct worker: %w", err)
	}

	s.worker = w
	s.statusChan = make(chan any, 3)
	s.exit = make(chan struct{})
	s.mu.Unlock()

	// Start worker
	if err := w.Start(); err != nil {
		return nil, fmt.Errorf("failed to start worker: %w", err)
	}

	s.log.Info("task queue worker started",
		zap.String("task_queue", s.config.TaskQueue),
		zap.Int("max_concurrent_activity", s.config.MaxConcurrentActivityExecution),
		zap.Int("max_concurrent_workflow", s.config.MaxConcurrentWorkflowExecution),
		zap.Int("workflows", len(s.workflows)),
		zap.Int("activities", len(s.activities)),
	)

	s.statusChan <- fmt.Sprintf("worker started for task queue: %s", s.config.TaskQueue)

	return s.statusChan, nil
}

// Stop implements supervisor.Service interface
func (s *TaskQueue) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.worker != nil {
		s.worker.Stop()
		s.worker = nil
		close(s.statusChan)
		close(s.exit)
		s.log.Info("task queue worker stopped", zap.String("task_queue", s.config.TaskQueue))
	}

	return nil
}

// RegisterWorkflow registers a workflow for later mounting
func (s *TaskQueue) RegisterWorkflow(name string, workflow interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.workflows[name]; exists {
		return fmt.Errorf("workflow '%s' already registered", name)
	}

	s.workflows[name] = workflow
	s.log.Debug("registered workflow",
		zap.String("task_queue", s.config.TaskQueue),
		zap.String("workflow", name),
	)
	return nil
}

// RegisterActivity registers an activity for later mounting
func (s *TaskQueue) RegisterActivity(name string, activity interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.activities[name]; exists {
		return fmt.Errorf("activity '%s' already registered", name)
	}

	s.activities[name] = activity
	s.log.Debug("registered activity",
		zap.String("task_queue", s.config.TaskQueue),
		zap.String("activity", name),
	)
	return nil
}

// GetRegisteredWorkflows returns a list of registered workflow names
func (s *TaskQueue) GetRegisteredWorkflows() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.workflows))
	for name := range s.workflows {
		names = append(names, name)
	}
	return names
}

// GetRegisteredActivities returns a list of registered activity names
func (s *TaskQueue) GetRegisteredActivities() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.activities))
	for name := range s.activities {
		names = append(names, name)
	}
	return names
}
