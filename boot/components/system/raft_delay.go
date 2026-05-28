// SPDX-License-Identifier: MPL-2.0

//go:build testdelay

package system

import (
	hraft "github.com/hashicorp/raft"

	"github.com/wippyai/runtime/system/topology/namereg/global"
)

// wrapFSM wraps the FSM with a DelayingFSM for integration testing.
// Active only when built with -tags testdelay.
func wrapFSM(fsm *global.FSM) hraft.FSM {
	return global.NewDelayingFSM(fsm)
}
