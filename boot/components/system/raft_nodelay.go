// SPDX-License-Identifier: MPL-2.0

//go:build !testdelay

package system

import (
	hraft "github.com/hashicorp/raft"

	"github.com/wippyai/runtime/system/globalreg"
)

// wrapFSM is a no-op in production builds — returns the FSM unchanged.
func wrapFSM(fsm *globalreg.FSM) hraft.FSM {
	return fsm
}
