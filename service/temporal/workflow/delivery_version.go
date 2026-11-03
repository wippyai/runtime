// SPDX-License-Identifier: MPL-2.0

package workflow

import "go.temporal.io/sdk/workflow"

const (
	eventDeliveryChangeID = "wippy/temporal/accepted-event-delivery"
	terminationChangeID   = "wippy/temporal/process-terminate"
	legacySignalLimit     = 10000
	legacyChildExitLimit  = 1000
)

// Only histories that exercised a former queue limit need a version marker.
// Decide at task dispatch, when Temporal's command sequencing is initialized,
// rather than inside the callbacks that collect historical events.
func (d *Definition) deliverAllAcceptedEvents() bool {
	return d.env.GetVersion(eventDeliveryChangeID, workflow.DefaultVersion, 1) != workflow.DefaultVersion
}

func (d *Definition) queueSignal(signal incomingSignal) {
	if len(d.signals)-len(d.legacySignalDrops) >= legacySignalLimit {
		d.legacySignalDrops = append(d.legacySignalDrops, len(d.signals))
	}
	d.signals = append(d.signals, signal)
}
