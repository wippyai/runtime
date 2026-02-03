package workflow

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/service/temporal/propagator"
	bindings "go.temporal.io/sdk/internalbindings"
	"go.uber.org/zap"
)

// Internal update response topics
const (
	updateTopicReject = "__reject__"
	updateTopicAck    = "ack"
	updateTopicNak    = "nak"
	updateTopicOk     = "ok"
	updateTopicError  = "error"
)

// updateState tracks the lifecycle of an update.
type updateState int

const (
	updatePending  updateState = iota // Waiting for ack/nak from workflow
	updateAccepted                    // Workflow sent ack, waiting for ok/error
	updateRejected                    // Workflow sent nak, update rejected
	updateComplete                    // Workflow sent ok/error, update completed
)

// updateEntry represents an update in progress.
type updateEntry struct {
	Callbacks bindings.UpdateCallbacks
	Name      string
	ID        string
	Payloads  payload.Payloads
	State     updateState
}

// UpdateManager manages workflow updates.
type UpdateManager struct {
	replayLog *propagator.ReplayLogger
	active    map[string]*updateEntry
	pending   []*updateEntry
}

// NewUpdateManager creates a new update manager.
func NewUpdateManager(replayLog *propagator.ReplayLogger) *UpdateManager {
	return &UpdateManager{
		replayLog: replayLog,
		active:    make(map[string]*updateEntry),
	}
}

// QueueUpdate queues an incoming update for delivery.
func (m *UpdateManager) QueueUpdate(name, id string, payloads payload.Payloads, callbacks bindings.UpdateCallbacks) {
	m.replayLog.Debug("QueueUpdate", zap.String("name", name), zap.String("id", id))

	upd := &updateEntry{
		Name:      name,
		ID:        id,
		Payloads:  payloads,
		State:     updatePending,
		Callbacks: callbacks,
	}
	m.pending = append(m.pending, upd)
}

// QueueRejection queues an update that should be immediately rejected.
func (m *UpdateManager) QueueRejection(id string, errMsg string, callbacks bindings.UpdateCallbacks) {
	upd := &updateEntry{
		Name:      updateTopicReject,
		ID:        id,
		Payloads:  payload.Payloads{payload.NewString(errMsg)},
		State:     updatePending,
		Callbacks: callbacks,
	}
	m.pending = append(m.pending, upd)
}

// HasPending returns true if there are pending updates.
func (m *UpdateManager) HasPending() bool {
	return len(m.pending) > 0
}

// FlushPending returns pending updates and clears the queue.
// Returns update entries that should be delivered as events.
func (m *UpdateManager) FlushPending() []*updateEntry {
	if len(m.pending) == 0 {
		return nil
	}

	result := make([]*updateEntry, 0, len(m.pending))
	for _, upd := range m.pending {
		if upd.Name == updateTopicReject {
			var errMsg string
			if len(upd.Payloads) > 0 {
				if s, ok := upd.Payloads[0].Data().(string); ok {
					errMsg = s
				}
			}
			if errMsg == "" {
				errMsg = "update decode error"
			}
			upd.State = updateRejected
			upd.Callbacks.Reject(errors.New(errMsg))
			continue
		}

		m.active[upd.ID] = upd
		result = append(result, upd)
	}
	m.pending = m.pending[:0]
	return result
}

// HandleResponse processes a response to an update (ack/nak/ok/error).
func (m *UpdateManager) HandleResponse(updateID, topic string, payloads payload.Payloads, resume func(data any, err error)) {
	m.replayLog.Debug("HandleResponse", zap.String("id", updateID), zap.String("topic", topic))

	upd, ok := m.active[updateID]
	if !ok {
		resume(nil, fmt.Errorf("unknown update: %s", updateID))
		return
	}

	switch topic {
	case updateTopicAck:
		m.handleAck(upd, resume)
	case updateTopicNak:
		m.handleNak(upd, payloads, resume)
	case updateTopicOk:
		m.handleOk(upd, payloads, resume)
	case updateTopicError:
		m.handleError(upd, payloads, resume)
	default:
		resume(nil, fmt.Errorf("unknown update response: %s (expected ack/nak/ok/error)", topic))
	}
}

func (m *UpdateManager) handleAck(upd *updateEntry, resume func(data any, err error)) {
	if upd.State != updatePending {
		resume(nil, fmt.Errorf("update already %s", stateString(upd.State)))
		return
	}
	upd.State = updateAccepted
	upd.Callbacks.Accept()
	resume(nil, nil)
}

func (m *UpdateManager) handleNak(upd *updateEntry, payloads payload.Payloads, resume func(data any, err error)) {
	if upd.State != updatePending {
		resume(nil, fmt.Errorf("update already %s", stateString(upd.State)))
		return
	}
	upd.State = updateRejected
	errMsg := extractErrorMessage(payloads, "update rejected")
	upd.Callbacks.Reject(errors.New(errMsg))
	delete(m.active, upd.ID)
	resume(nil, nil)
}

func (m *UpdateManager) handleOk(upd *updateEntry, payloads payload.Payloads, resume func(data any, err error)) {
	if upd.State != updateAccepted {
		resume(nil, fmt.Errorf("update not accepted (state: %s)", stateString(upd.State)))
		return
	}
	upd.State = updateComplete

	var result any
	if len(payloads) > 0 {
		p := payloads[0]
		if p.Format() == payload.JSON {
			jsonBytes, ok := p.Data().([]byte)
			if !ok {
				resume(nil, fmt.Errorf("json payload data is not []byte"))
				return
			}
			if err := json.Unmarshal(jsonBytes, &result); err != nil {
				resume(nil, fmt.Errorf("failed to unmarshal JSON: %w", err))
				return
			}
		} else {
			result = p.Data()
		}
	}

	upd.Callbacks.Complete(result, nil)
	delete(m.active, upd.ID)
	resume(nil, nil)
}

func (m *UpdateManager) handleError(upd *updateEntry, payloads payload.Payloads, resume func(data any, err error)) {
	if upd.State != updateAccepted {
		resume(nil, fmt.Errorf("update not accepted (state: %s)", stateString(upd.State)))
		return
	}
	upd.State = updateComplete
	errMsg := extractErrorMessage(payloads, "update failed")
	upd.Callbacks.Complete(nil, errors.New(errMsg))
	delete(m.active, upd.ID)
	resume(nil, nil)
}

// stateString returns the string representation of an update state.
func stateString(s updateState) string {
	switch s {
	case updatePending:
		return "pending"
	case updateAccepted:
		return "accepted"
	case updateRejected:
		return "rejected"
	case updateComplete:
		return "completed"
	default:
		return "unknown"
	}
}

// extractErrorMessage extracts an error message from payloads.
func extractErrorMessage(payloads payload.Payloads, defaultMsg string) string {
	if len(payloads) > 0 {
		if s, ok := payloads[0].Data().(string); ok {
			return s
		}
		return fmt.Sprintf("%v", payloads[0].Data())
	}
	return defaultMsg
}
