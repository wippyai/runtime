// SPDX-License-Identifier: MPL-2.0

//go:build !testdelay

package system

import (
	hraft "github.com/hashicorp/raft"

	"github.com/wippyai/runtime/system/topology/namereg/global"
)

// wrapFSM is a no-op in production builds — returns the FSM unchanged.
func wrapFSM(fsm *global.FSM) hraft.FSM {
	return fsm
}
