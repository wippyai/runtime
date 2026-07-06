// SPDX-License-Identifier: MPL-2.0

package attempt

import (
	"github.com/wippyai/runtime/service/cloudgraph/resource"
)

// createPhaseRank orders the create-action signal ladder. Providers may skip
// intermediate phases but never move backward within an incarnation
// (design §9.5, C1 subset for create operations).
var createPhaseRank = map[resource.SignalPhaseName]int{
	resource.PhaseAllocateStarted:    1,
	resource.PhaseAllocateCommitted:  2,
	resource.PhaseConfigureStarted:   3,
	resource.PhaseConfigureCommitted: 4,
	resource.PhaseVerifyStarted:      5,
	resource.PhaseVerifyPassed:       6,
	resource.PhaseVerifyFailed:       6,
}

// stepOutcome is the FSM decision for one accepted signal.
type stepOutcome int

// Step outcomes.
const (
	stepContinue stepOutcome = iota
	stepCommit
	stepFailed
)

// signalFSM tracks the in-incarnation signal ladder position. The sequence
// fence (C4) lives in the ledger; this covers phase legality (C1).
type signalFSM struct {
	rank int
}

// step validates one signal against the create ladder and returns the
// outcome. An illegal signal is an error, never silently absorbed.
func (f *signalFSM) step(sig resource.Signal) (stepOutcome, error) {
	switch sig.Kind {
	case resource.SignalCheckpoint, resource.SignalOutput:
		return stepContinue, nil

	case resource.SignalFailed:
		return stepFailed, nil

	case resource.SignalPhase:
		rank, ok := createPhaseRank[sig.Phase]
		if !ok {
			return stepContinue, NewIllegalSignalError(string(sig.Phase) + " is not a create phase")
		}
		if rank <= f.rank {
			return stepContinue, NewIllegalSignalError(string(sig.Phase) + " moves the ladder backward")
		}
		f.rank = rank

		switch sig.Phase {
		case resource.PhaseVerifyPassed:
			return stepCommit, nil
		case resource.PhaseVerifyFailed:
			return stepFailed, nil
		default:
			return stepContinue, nil
		}

	default:
		return stepContinue, NewIllegalSignalError("unknown signal kind: " + string(sig.Kind))
	}
}
