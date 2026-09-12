// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
)

const monitorCompletionAckKind = "pid.monitor.complete.ack"

// A completion ACK settles only this exact delivery obligation. It proves
// recipient acceptance (or an obsolete recipient relationship), never durable
// application work. No caller-provided peer identity can authorize settlement.
type monitorCompletionAck struct {
	caller, target pid.PID
	reference      string
}

func (a monitorCompletionAck) packageForPeer() *relay.Package {
	return relay.NewPackage(a.caller, pid.PID{Node: a.target.Node, Host: monitorControlHostID}, topapi.TopicEvents, payload.New(map[string]any{
		"v": remoteMonitorVersion, "kind": monitorCompletionAckKind,
		"ref": a.reference, "caller": a.caller, "target": a.target,
	}))
}

// decodeMonitorCompletionAck borrows the package and validates the whole control
// envelope before an outbox may consult its exact retained reference.
func decodeMonitorCompletionAck(pkg *relay.Package, local pid.NodeID) (monitorCompletionAck, error) {
	invalid := func() (monitorCompletionAck, error) {
		return monitorCompletionAck{}, errors.New("invalid monitor completion acknowledgment")
	}
	if pkg == nil || local == "" || len(pkg.Messages) != 1 || pkg.Messages[0] == nil || pkg.Messages[0].Topic != topapi.TopicEvents || len(pkg.Messages[0].Payloads) != 1 || pkg.Messages[0].Payloads[0] == nil || !pkg.Target.Equal(pid.PID{Node: local, Host: monitorControlHostID}) {
		return invalid()
	}
	fields, ok := pkg.Messages[0].Payloads[0].Data().(map[string]any)
	if !ok || len(fields) != 5 || fields["kind"] != monitorCompletionAckKind {
		return invalid()
	}
	validVersion := false
	switch v := fields["v"].(type) {
	case uint64:
		validVersion = v == remoteMonitorVersion
	case int64:
		validVersion = v == int64(remoteMonitorVersion)
	}
	caller, callerOK := fields["caller"].(pid.PID)
	target, targetOK := fields["target"].(pid.PID)
	ref, refOK := fields["ref"].(string)
	if !validVersion || !callerOK || !targetOK || !refOK || len(ref) == 0 || len(ref) > 64 || pkg.ReceivedFrom == "" || caller.Node != pkg.ReceivedFrom || caller.Node == local || !caller.Equal(pkg.Source) || target.Node != local || target.Host == "" || target.UniqID == "" {
		return invalid()
	}
	return monitorCompletionAck{caller: caller, target: target, reference: ref}, nil
}

// receiveCompletionAck linearizes settlement with endpoint shutdown. It does
// not hold endpoint/outbox locks across package-release callbacks. Lock order
// is endpoint -> outbox; outbox methods never call endpoint code.
func (x *monitorExchange) receiveCompletionAck(ctx context.Context, pkg *relay.Package) error {
	ack, err := decodeMonitorCompletionAck(pkg, x.local)
	if err != nil {
		return err
	}
	x.mu.Lock()
	if x.stopped || x.ctx.Err() != nil {
		x.mu.Unlock()
		return context.Canceled
	}
	if err := ctx.Err(); err != nil {
		x.mu.Unlock()
		return err
	}
	if x.terminalOutbox == nil {
		x.mu.Unlock()
		return errors.New("monitor terminal outbox is not installed")
	}
	x.terminalOutbox.ack(ack.target, ack.caller, ack.reference)
	x.mu.Unlock()
	// A valid stale, duplicate or premature ACK is consumed but cannot settle
	// another record. On refusal above, the caller retains package ownership.
	relay.ReleasePackage(pkg)
	return nil
}
