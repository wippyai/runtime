// SPDX-License-Identifier: MPL-2.0

package attempt

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/service/cloudgraph/resource"
)

func phaseSignal(phase resource.SignalPhaseName) resource.Signal {
	return resource.Signal{Kind: resource.SignalPhase, Phase: phase}
}

func TestFSMHappyLadder(t *testing.T) {
	fsm := &signalFSM{}

	for _, phase := range []resource.SignalPhaseName{
		resource.PhaseAllocateStarted,
		resource.PhaseAllocateCommitted,
		resource.PhaseConfigureStarted,
		resource.PhaseConfigureCommitted,
		resource.PhaseVerifyStarted,
	} {
		outcome, err := fsm.step(phaseSignal(phase))
		require.NoError(t, err, string(phase))
		require.Equal(t, stepContinue, outcome, string(phase))
	}

	outcome, err := fsm.step(phaseSignal(resource.PhaseVerifyPassed))
	require.NoError(t, err)
	require.Equal(t, stepCommit, outcome)
}

func TestFSMSkippedPhasesAllowed(t *testing.T) {
	fsm := &signalFSM{}

	outcome, err := fsm.step(phaseSignal(resource.PhaseAllocateStarted))
	require.NoError(t, err)
	require.Equal(t, stepContinue, outcome)

	outcome, err = fsm.step(phaseSignal(resource.PhaseConfigureCommitted))
	require.NoError(t, err)
	require.Equal(t, stepContinue, outcome)

	outcome, err = fsm.step(phaseSignal(resource.PhaseVerifyPassed))
	require.NoError(t, err)
	require.Equal(t, stepCommit, outcome)
}

func TestFSMBackwardPhaseRejected(t *testing.T) {
	fsm := &signalFSM{}

	_, err := fsm.step(phaseSignal(resource.PhaseConfigureCommitted))
	require.NoError(t, err)

	_, err = fsm.step(phaseSignal(resource.PhaseAllocateStarted))
	require.Error(t, err)

	_, err = fsm.step(phaseSignal(resource.PhaseConfigureCommitted))
	require.Error(t, err, "phase repeat must be rejected")
}

func TestFSMDeletePhasesRejectedForCreate(t *testing.T) {
	fsm := &signalFSM{}

	_, err := fsm.step(phaseSignal(resource.PhaseDeleteStarted))
	require.Error(t, err)

	_, err = fsm.step(phaseSignal(resource.PhaseDeleteCommitted))
	require.Error(t, err)
}

func TestFSMTerminalsAndEnvelopes(t *testing.T) {
	fsm := &signalFSM{}

	outcome, err := fsm.step(resource.Signal{Kind: resource.SignalCheckpoint})
	require.NoError(t, err)
	require.Equal(t, stepContinue, outcome)

	outcome, err = fsm.step(resource.Signal{Kind: resource.SignalOutput})
	require.NoError(t, err)
	require.Equal(t, stepContinue, outcome)

	outcome, err = fsm.step(phaseSignal(resource.PhaseVerifyFailed))
	require.NoError(t, err)
	require.Equal(t, stepFailed, outcome)

	outcome, err = fsm.step(resource.Signal{Kind: resource.SignalFailed})
	require.NoError(t, err)
	require.Equal(t, stepFailed, outcome)

	_, err = fsm.step(resource.Signal{Kind: "bogus"})
	require.Error(t, err)

	_, err = fsm.step(resource.Signal{Kind: resource.SignalPhase, Phase: "bogus"})
	require.Error(t, err)
}
