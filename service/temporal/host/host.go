package host

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/service/temporal"
	tmcli "go.temporal.io/sdk/client"
)

type TemporalHost struct {
	hostID    relay.HostID
	taskQueue string
	client    tmcli.Client
	workflows map[string]*temporal.WorkflowRegistration
	mu        sync.RWMutex
}

func NewTemporalHost(hostID relay.HostID, taskQueue string, client tmcli.Client) *TemporalHost {
	return &TemporalHost{
		hostID:    hostID,
		taskQueue: taskQueue,
		client:    client,
		workflows: make(map[string]*temporal.WorkflowRegistration),
	}
}

func (h *TemporalHost) Launch(ctx context.Context, launch *process.Launch) (relay.PID, error) {
	sourceKey := launch.Source.String()
	h.mu.RLock()
	wf, exists := h.workflows[sourceKey]
	h.mu.RUnlock()

	if !exists {
		return relay.PID{}, fmt.Errorf("workflow not found: %s", sourceKey)
	}

	if wf.Options == nil {
		return relay.PID{}, fmt.Errorf("workflow %s has no options", wf.Name)
	}

	// Generate UUID for workflow execution ID
	workflowID := uuid.New().String()

	options := *wf.Options
	options.ID = workflowID
	options.TaskQueue = h.taskQueue

	run, err := h.client.ExecuteWorkflow(ctx, options, wf.Name, launch.Input)
	if err != nil {
		return relay.PID{}, fmt.Errorf("failed to execute workflow: %w", err)
	}

	pid := relay.PID{
		Node:   launch.PID.Node,
		Host:   h.hostID,
		UniqID: run.GetID(),
	}

	return pid, nil
}

func (h *TemporalHost) Send(pkg *relay.Package) error {
	for _, msg := range pkg.Messages {
		err := h.client.SignalWorkflow(context.Background(), pkg.Target.UniqID, "", msg.Topic, msg.Payloads)
		if err != nil {
			return fmt.Errorf("failed to signal workflow: %w", err)
		}
	}
	return nil
}

func (h *TemporalHost) Terminate(ctx context.Context, pid relay.PID) error {
	return h.client.TerminateWorkflow(ctx, pid.UniqID, "", "terminated by user")
}

func (h *TemporalHost) RegisterWorkflow(reg *temporal.WorkflowRegistration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.workflows[reg.Source.String()] = reg
}
