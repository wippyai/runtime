package activity

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/executor"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

// executeActivity handles the actual execution of an activity through the runtime executor
func (m *Manager) executeActivity(ctx context.Context, id registry.ID, inputs payload.Payloads) (payload.Payloads, error) {
	m.mu.RLock()
	ah, exists := m.handlers[id]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("activity handler %s not found", id)
	}

	// Create a task for the executor
	task := executor.Task{
		Context:  ctx,
		Target:   registry.ID(ah.config.Function), // Use the function target from config
		Payloads: inputs,                          // Pass through the input payloads
	}

	// Get the task
	resultCh, err := m.executor.Execute(task)
	if err != nil {
		return nil, fmt.Errorf("failed to execute activity task: %w", err)
	}

	// Wait for result with context cancellation handling
	select {
	case result := <-resultCh:
		if result == nil {
			return nil, fmt.Errorf("received nil result from executor")
		}
		if result.Error != nil {
			return nil, fmt.Errorf("activity execution failed: %w", result.Error)
		}

		// Convert single payload to payloads slice if needed
		if result.Payload != nil {
			return payload.Payloads{result.Payload}, nil
		}

		return payload.Payloads{}, nil

	case <-ctx.Done():
		return nil, fmt.Errorf("activity execution cancelled: %w", ctx.Err())
	}
}
