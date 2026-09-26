// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology"
)

// newPeerTargetNotRegisteredError reports a peer-node relationship request
// for a target that is not registered on this node.
func newPeerTargetNotRegisteredError(operation string, target, peer pid.PID) apierror.Error {
	return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
		"pid":       target.String(),
		"operation": operation,
		"peer":      peer.String(),
	})
}
