package workflow

import (
	"fmt"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/service/temporal/propagator"
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
	d.signals = append(d.signals, incomingSignal{
		Name:     topology.TopicEvents,
		Payloads: payload.Payloads{payload.New(cancelEvent)},
	})
}

// handleSignal queues incoming signals for delivery to the process.
func (d *Definition) handleSignal(name string, input *commonpb.Payloads, header *commonpb.Header) error {
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
	var from pid.PID
	if header != nil {
		values, err := propagator.ExtractFromHeader(d.dc, header)
		if err != nil {
			d.replayLog.Warn("failed to extract signal header", zap.Error(err))
		} else if val, ok := values[propagator.SignalFromValueKey]; ok {
			if fromStr, ok := val.(string); ok && fromStr != "" {
				if parsed, err := pid.ParsePID(fromStr); err == nil {
					from = parsed
				} else {
					d.replayLog.Warn("failed to parse signal sender pid", zap.String("value", fromStr), zap.Error(err))
				}
			}
		}
	}

	d.signals = append(d.signals, incomingSignal{Name: name, Payloads: payloads, From: from})
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
