// Package runref provides a one-shot handoff mechanism for passing workflow
// run references between the start caller and monitor/link setup.
package runref

import "sync"

type handoffKey struct {
	clientID   string
	workflowID string
}

// Handoff is a one-shot map for transferring workflow run IDs from start to monitor/link watcher setup.
type Handoff struct {
	runs map[handoffKey]string
	mu   sync.Mutex
}

// NewHandoff creates an empty workflow run handoff registry.
func NewHandoff() *Handoff {
	return &Handoff{
		runs: make(map[handoffKey]string),
	}
}

// Publish stores a run ID for later consumption.
func (h *Handoff) Publish(clientID, workflowID, runID string) {
	if h == nil || clientID == "" || workflowID == "" || runID == "" {
		return
	}
	h.mu.Lock()
	h.runs[handoffKey{clientID: clientID, workflowID: workflowID}] = runID
	h.mu.Unlock()
}

// Consume returns and removes the run ID for a workflow, if present.
func (h *Handoff) Consume(clientID, workflowID string) (string, bool) {
	if h == nil || clientID == "" || workflowID == "" {
		return "", false
	}
	key := handoffKey{clientID: clientID, workflowID: workflowID}
	h.mu.Lock()
	runID, ok := h.runs[key]
	if ok {
		delete(h.runs, key)
	}
	h.mu.Unlock()
	return runID, ok
}
