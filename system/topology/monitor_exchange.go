// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
)

const monitorControlHostID = "topology"
const monitorResultKind = "pid.monitor.result"

type monitorReplyKey struct{ reference, operation string }
type monitorPendingReply struct {
	target pid.PID
	result chan error
}

// monitorExchange owns bounded caller waits and completion deliveries. Reply
// and ACK paths settle correlations without network or application IO; terminal
// delivery uses separately bounded slots when entering the local consumer.
type monitorExchange struct {
	// terminalOutbox is installed before the endpoint is published. ACK handling
	// performs no network IO and does not consume application delivery slots.
	terminalOutbox *monitorOutbox
	nativePending  int
	onCompletion   func(context.Context, *relay.Package, map[string]any) error
	deliveries     int
	mu             sync.Mutex
	ctx            context.Context
	cancel         context.CancelFunc
	sender         relay.ContextSender
	local          pid.NodeID
	timeout        time.Duration
	maximum        int
	pending        map[monitorReplyKey]monitorPendingReply
	stopped        bool
	calls          sync.WaitGroup
}

func newMonitorExchange(ctx context.Context, local pid.NodeID, sender relay.ContextSender, maximum int, timeout time.Duration) (*monitorExchange, error) {
	if ctx == nil || local == "" || sender == nil || maximum <= 0 || timeout <= 0 {
		return nil, errors.New("invalid monitor exchange configuration")
	}
	owned, cancel := context.WithCancel(ctx)
	return &monitorExchange{ctx: owned, cancel: cancel, local: local, sender: sender, maximum: maximum, timeout: timeout, pending: make(map[monitorReplyKey]monitorPendingReply)}, nil
}

func (x *monitorExchange) roundTrip(ctx context.Context, control remoteMonitorControl) error {
	if ctx == nil {
		return errors.New("monitor request requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if control.caller.Node != x.local || control.target.Node == "" || control.target.Node == x.local || control.target.Host == "" || control.target.UniqID == "" || control.reference == "" || len(control.reference) > 64 || (control.kind != topapi.MonitorRequest && control.kind != topapi.MonitorRelease) {
		return errors.New("invalid monitor request")
	}
	if (control.kind == topapi.MonitorRequest && (len(control.previous) > 64 || control.previous == control.reference)) || (control.kind == topapi.MonitorRelease && control.previous != "") {
		return errors.New("invalid monitor predecessor")
	}
	key := monitorReplyKey{control.reference, control.kind}
	reply := monitorPendingReply{target: control.target, result: make(chan error, 1)}
	x.mu.Lock()
	if x.stopped || x.ctx.Err() != nil {
		x.mu.Unlock()
		return context.Canceled
	}
	if _, exists := x.pending[key]; exists {
		x.mu.Unlock()
		return errRemoteMonitorConflict
	}
	if len(x.pending) >= x.maximum {
		x.mu.Unlock()
		return errRemoteMonitorCapacity
	}
	x.pending[key] = reply
	x.calls.Add(1)
	x.mu.Unlock()
	defer func() { x.mu.Lock(); delete(x.pending, key); x.mu.Unlock(); x.calls.Done() }()
	ctx, cancel := context.WithTimeout(ctx, x.timeout)
	stop := context.AfterFunc(x.ctx, cancel)
	defer stop()
	defer cancel()
	fields := map[string]any{"v": remoteMonitorVersion, "kind": control.kind, "ref": control.reference, "caller": control.caller, "target": control.target}
	if control.kind == topapi.MonitorRequest {
		fields["previous"] = control.previous
	}
	pkg := relay.NewPackage(control.caller, control.target, topapi.TopicEvents, payload.New(fields))
	if err := x.sender.SendContext(ctx, pkg); err != nil {
		relay.ReleasePackage(pkg)
		return err
	}
	select {
	case err := <-reply.result:
		return err
	case <-ctx.Done():
		// A completed result wins over simultaneous cancellation. Otherwise this
		// is uncertainty, not proof that target installation was rolled back.
		select {
		case err := <-reply.result:
			return err
		default:
			return ctx.Err()
		}
	}
}

func (x *monitorExchange) Send(pkg *relay.Package) error {
	return x.SendContext(context.Background(), pkg)
}
func (x *monitorExchange) SendContext(ctx context.Context, pkg *relay.Package) error {
	if ctx == nil {
		return errors.New("monitor reply requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if pkg == nil || len(pkg.Messages) != 1 || pkg.Messages[0] == nil || pkg.Messages[0].Topic != topapi.TopicEvents || len(pkg.Messages[0].Payloads) != 1 || pkg.Messages[0].Payloads[0] == nil || !pkg.Target.Equal(pid.PID{Node: x.local, Host: monitorControlHostID}) {
		return errors.New("invalid monitor reply package")
	}
	fields, ok := pkg.Messages[0].Payloads[0].Data().(map[string]any)
	if ok && fields["kind"] == monitorCompletionAckKind {
		return x.receiveCompletionAck(ctx, pkg)
	}
	if ok && fields["kind"] == topapi.Exit && x.onCompletion != nil {
		return x.withCompletion(ctx, func(bounded context.Context) error { return x.onCompletion(bounded, pkg, fields) })
	}
	if !ok || len(fields) != 5 || fields["kind"] != monitorResultKind {
		return errors.New("invalid monitor reply")
	}
	validVersion := false
	switch v := fields["v"].(type) {
	case uint64:
		validVersion = v == remoteMonitorVersion
	case int64:
		validVersion = v == int64(remoteMonitorVersion)
	}
	reference, refOK := fields["ref"].(string)
	operation, opOK := fields["operation"].(string)
	code, codeOK := fields["code"].(string)
	if !validVersion || !refOK || reference == "" || len(reference) > 64 || !opOK || (operation != topapi.MonitorRequest && operation != topapi.MonitorRelease) || !codeOK {
		return errors.New("invalid monitor reply fields")
	}
	result, valid := monitorResultError(code)
	if !valid {
		return errors.New("unknown monitor reply result")
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.stopped {
		return context.Canceled
	}
	pending, exists := x.pending[monitorReplyKey{reference, operation}]
	if !exists {
		relay.ReleasePackage(pkg)
		return nil
	}
	if pkg.ReceivedFrom == "" || pkg.ReceivedFrom != pending.target.Node || !pkg.Source.Equal(pending.target) {
		return errors.New("monitor reply peer mismatch")
	}
	select {
	case pending.result <- result:
	default:
	}
	relay.ReleasePackage(pkg)
	return nil
}

func (x *monitorExchange) stop() error {
	x.mu.Lock()
	x.stopped = true
	x.cancel()
	x.mu.Unlock()
	x.calls.Wait()
	return x.terminalOutbox.stop()
}

func monitorResultError(code string) (error, bool) {
	switch code {
	case "ok":
		return nil, true
	case "missing":
		return topapi.ErrPIDNotRegistered, true
	case "conflict":
		return errRemoteMonitorConflict, true
	case "capacity":
		return errRemoteMonitorCapacity, true
	case "closed":
		return errRemoteMonitorClosed, true
	default:
		return nil, false
	}
}

func (x *monitorExchange) withCompletion(ctx context.Context, deliver func(context.Context) error) error {
	if ctx == nil {
		return errors.New("monitor completion requires context")
	}
	x.mu.Lock()
	if x.stopped {
		x.mu.Unlock()
		return context.Canceled
	}
	if x.deliveries >= x.maximum {
		x.mu.Unlock()
		return errRemoteMonitorCapacity
	}
	x.deliveries++
	x.calls.Add(1)
	x.mu.Unlock()
	defer func() { x.mu.Lock(); x.deliveries--; x.mu.Unlock(); x.calls.Done() }()
	bounded, cancel := context.WithTimeout(ctx, x.timeout)
	stop := context.AfterFunc(x.ctx, cancel)
	defer stop()
	defer cancel()
	return deliver(bounded)
}
