package workflow_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/service/temporal/workflow"
	"go.uber.org/zap"
)

// mockWorkerRegistry tracks workflow registrations for testing
type mockWorkerRegistry struct {
	registeredWorkflows map[string]registeredWorkflow
}

type registeredWorkflow struct {
	handler  any
	workerID string
}

func newMockWorkerRegistry() *mockWorkerRegistry {
	return &mockWorkerRegistry{
		registeredWorkflows: make(map[string]registeredWorkflow),
	}
}

func (m *mockWorkerRegistry) RegisterWorkflow(_ context.Context, workerID registry.ID, workflowName string, handler any) error {
	m.registeredWorkflows[workflowName] = registeredWorkflow{
		workerID: workerID.String(),
		handler:  handler,
	}
	return nil
}

func (m *mockWorkerRegistry) UnregisterWorkflow(_ context.Context, _ registry.ID, workflowName string) error {
	delete(m.registeredWorkflows, workflowName)
	return nil
}

// TestWorkflowListenerRegistration tests that the workflow listener correctly
// identifies and registers workflows based on metadata.
func TestWorkflowListenerRegistration(t *testing.T) {
	log := zap.NewNop()

	mockWorkers := newMockWorkerRegistry()
	listener := workflow.NewListener(log, mockWorkers)

	// Create entry with temporal workflow metadata
	entry := registry.Entry{
		ID:   registry.ID{NS: "app.test", Name: "my_workflow"},
		Kind: "workflow.lua",
		Meta: createWorkflowMeta("app.test:worker"),
	}

	// Add entry
	err := listener.Add(context.Background(), entry)
	require.NoError(t, err)

	// Verify workflow was registered
	require.Contains(t, mockWorkers.registeredWorkflows, "app.test:my_workflow")
	registered := mockWorkers.registeredWorkflows["app.test:my_workflow"]
	require.Equal(t, "app.test:worker", registered.workerID)

	// Verify handler is a DefinitionFactory
	factory, ok := registered.handler.(*workflow.DefinitionFactory)
	require.True(t, ok, "handler should be a *DefinitionFactory")
	require.Equal(t, "app.test:my_workflow", factory.ID.String())
}

// TestWorkflowListenerIgnoresNonWorkflows tests that non-workflow entries are ignored
func TestWorkflowListenerIgnoresNonWorkflows(t *testing.T) {
	log := zap.NewNop()

	mockWorkers := newMockWorkerRegistry()
	listener := workflow.NewListener(log, mockWorkers)

	// Create function entry (not workflow)
	entry := registry.Entry{
		ID:   registry.ID{NS: "app.test", Name: "my_function"},
		Kind: "function.lua",
		Meta: createWorkflowMeta("app.test:worker"),
	}

	err := listener.Add(context.Background(), entry)
	require.NoError(t, err)

	// Verify no workflow was registered
	require.Empty(t, mockWorkers.registeredWorkflows)
}

// TestWorkflowListenerIgnoresMissingMeta tests that entries without workflow metadata are ignored
func TestWorkflowListenerIgnoresMissingMeta(t *testing.T) {
	log := zap.NewNop()

	mockWorkers := newMockWorkerRegistry()
	listener := workflow.NewListener(log, mockWorkers)

	// Create workflow entry without metadata
	entry := registry.Entry{
		ID:   registry.ID{NS: "app.test", Name: "my_workflow"},
		Kind: "workflow.lua",
		Meta: nil,
	}

	err := listener.Add(context.Background(), entry)
	require.NoError(t, err)

	// Verify no workflow was registered
	require.Empty(t, mockWorkers.registeredWorkflows)
}

// TestWorkflowListenerDelete tests workflow unregistration
func TestWorkflowListenerDelete(t *testing.T) {
	log := zap.NewNop()

	mockWorkers := newMockWorkerRegistry()
	listener := workflow.NewListener(log, mockWorkers)

	entry := registry.Entry{
		ID:   registry.ID{NS: "app.test", Name: "my_workflow"},
		Kind: "workflow.lua",
		Meta: createWorkflowMeta("app.test:worker"),
	}

	// Add entry
	err := listener.Add(context.Background(), entry)
	require.NoError(t, err)
	require.Contains(t, mockWorkers.registeredWorkflows, "app.test:my_workflow")

	// Delete entry
	err = listener.Delete(context.Background(), entry)
	require.NoError(t, err)
	require.NotContains(t, mockWorkers.registeredWorkflows, "app.test:my_workflow")
}

// TestWorkflowListenerDefaultNamespace tests that worker ID inherits namespace if not specified
func TestWorkflowListenerDefaultNamespace(t *testing.T) {
	log := zap.NewNop()

	mockWorkers := newMockWorkerRegistry()
	listener := workflow.NewListener(log, mockWorkers)

	// Create entry with worker ID without namespace
	entry := registry.Entry{
		ID:   registry.ID{NS: "app.test", Name: "my_workflow"},
		Kind: "workflow.lua",
		Meta: createWorkflowMeta("worker"), // no namespace
	}

	err := listener.Add(context.Background(), entry)
	require.NoError(t, err)

	// Verify workflow was registered with inherited namespace
	require.Contains(t, mockWorkers.registeredWorkflows, "app.test:my_workflow")
	registered := mockWorkers.registeredWorkflows["app.test:my_workflow"]
	require.Equal(t, "app.test:worker", registered.workerID)
}

// Helper to create workflow metadata
func createWorkflowMeta(workerID string) attrs.Bag {
	meta := attrs.NewBag()
	meta.Set("temporal", map[string]any{
		"workflow": map[string]any{
			"worker": workerID,
		},
	})
	return meta
}
