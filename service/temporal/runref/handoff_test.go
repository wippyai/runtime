// SPDX-License-Identifier: MPL-2.0

package runref

import "testing"

func TestHandoff_PublishConsume(t *testing.T) {
	h := NewHandoff()

	h.Publish("client-1", "wf-1", "run-1")

	runID, ok := h.Consume("client-1", "wf-1")
	if !ok {
		t.Fatal("expected handoff entry")
	}
	if runID != "run-1" {
		t.Fatalf("unexpected runID: %s", runID)
	}

	_, ok = h.Consume("client-1", "wf-1")
	if ok {
		t.Fatal("expected one-shot consume")
	}
}

func TestHandoff_RejectsInvalidInputs(t *testing.T) {
	h := NewHandoff()

	h.Publish("", "wf-1", "run-1")
	h.Publish("client-1", "", "run-1")
	h.Publish("client-1", "wf-1", "")

	_, ok := h.Consume("client-1", "wf-1")
	if ok {
		t.Fatal("expected no stored run for invalid publish input")
	}
}
