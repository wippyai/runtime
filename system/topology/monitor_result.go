// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"errors"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
)

// PrepareRemoteMonitorReply borrows the request and returns an owned response
// for a validated request. The host must reserve bounded reply capacity BEFORE
// invoking it and must release/send the returned response exactly once. Invalid
// or unauthenticated requests do not produce a reflected reply.
func (t *Topology) PrepareRemoteMonitorReply(pkg *relay.Package, maximum int) (bool, *relay.Package, error) {
	control, err := decodeRemoteMonitor(pkg, t.localNodeID)
	if err != nil {
		return true, nil, err
	}
	if control == nil {
		return false, nil, nil
	}
	_, result := t.HandleRemoteMonitor(pkg, maximum)
	code := ""
	switch {
	case result == nil:
		code = "ok"
	case errors.Is(result, topapi.ErrPIDNotRegistered):
		code = "missing"
	case errors.Is(result, errRemoteMonitorConflict):
		code = "conflict"
	case errors.Is(result, errRemoteMonitorCapacity):
		code = "capacity"
	case errors.Is(result, errRemoteMonitorClosed):
		code = "closed"
	default:
		return true, nil, result
	}
	reply := relay.NewPackage(control.target, pid.PID{Node: control.caller.Node, Host: monitorControlHostID}, topapi.TopicEvents, payload.New(map[string]any{
		"v": remoteMonitorVersion, "kind": monitorResultKind, "operation": control.kind, "ref": control.reference, "code": code,
	}))
	return true, reply, nil
}
