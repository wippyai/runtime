package workflow

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/topology"
	commonpb "go.temporal.io/api/common/v1"
	bindings "go.temporal.io/sdk/internalbindings"
	"go.uber.org/zap"
)

// handleCancel is called when the workflow receives a cancellation request.
func (d *Definition) handleCancel() {
	d.canceled = true
	cancelEvent := map[string]any{
		"at":   d.env.Now().Format(time.RFC3339),
		"kind": topology.Cancel,
		"from": topology.SystemPID.String(),
	}
	jsonBytes, err := json.Marshal(cancelEvent)
	if err != nil {
		d.replayLog.Error("failed to marshal cancel event", zap.Error(err))
		return
	}
	d.signals = append(d.signals, incomingSignal{
		Name:     topology.TopicEvents,
		Payloads: payload.Payloads{payload.NewPayload(jsonBytes, payload.JSON)},
	})
}

// handleSignal queues incoming signals for delivery to the process.
func (d *Definition) handleSignal(name string, input *commonpb.Payloads, _ *commonpb.Header) error {
	if len(d.signals) >= maxSignalQueueSize {
		d.replayLog.Warn("signal queue full, dropping signal", zap.String("name", name))
		return nil
	}
	var payloads payload.Payloads
	if input != nil {
		if err := d.dc.FromPayloads(input, &payloads); err != nil {
			return fmt.Errorf("failed to decode signal payloads: %w", err)
		}
	}
	d.signals = append(d.signals, incomingSignal{Name: name, Payloads: payloads})
	return nil
}

// handleQuery handles incoming queries by returning the queryable state.
func (d *Definition) handleQuery(queryType string, _ *commonpb.Payloads, _ *commonpb.Header) (*commonpb.Payloads, error) {
	switch queryType {
	case "state":
		result := d.queryState
		if result == nil {
			result = make(map[string]any)
		}
		return d.dc.ToPayloads(result)
	case "pid":
		return d.dc.ToPayloads(d.env.WorkflowInfo().WorkflowExecution.ID)
	default:
		if val, ok := d.queryState[queryType]; ok {
			return d.dc.ToPayloads(val)
		}
		return nil, fmt.Errorf("unknown query type: %s", queryType)
	}
}

// handleUpdate queues incoming updates for delivery to the workflow.
func (d *Definition) handleUpdate(name string, id string, input *commonpb.Payloads, _ *commonpb.Header, callbacks bindings.UpdateCallbacks) {
	var payloads payload.Payloads
	if input != nil {
		if err := d.dc.FromPayloads(input, &payloads); err != nil {
			d.replayLog.Error("handleUpdate decode failed", zap.Error(err))
			d.updates.QueueRejection(id, err.Error(), callbacks)
			return
		}
	}
	d.updates.QueueUpdate(name, id, payloads, callbacks)
}
