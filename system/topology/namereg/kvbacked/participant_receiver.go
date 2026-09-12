// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

const (
	participantSnapshotRequestTopic  = "naming.snapshot.request"
	participantSnapshotResponseTopic = "naming.snapshot.response"
)

// participantReceiver owns bounded accepted relay work through reply admission,
// not merely snapshot capture. A successful Send transfers package ownership;
// refusal leaves it with the caller.
type participantReceiver struct {
	authority            *participantAuthority
	router               relay.ContextSender
	maxBytes, maxEntries int
	timeout              time.Duration
	ctx                  context.Context
	cancel               context.CancelFunc
	mu                   sync.Mutex
	stopping             bool
	slots                chan struct{}
	refusals             chan struct{} // bounded separately so blocked captures can report busy
	work                 sync.WaitGroup
	stopOnce             sync.Once
	done                 chan struct{}
}

func newParticipantReceiver(authority *participantAuthority, router relay.ContextSender, maxBytes, maxEntries int, timeout time.Duration) (*participantReceiver, error) {
	if authority == nil || router == nil || maxBytes <= 0 || maxEntries <= 0 || timeout <= 0 {
		return nil, fmt.Errorf("invalid participant receiver configuration")
	}
	ctx, cancel := context.WithCancel(authority.ctx)
	return &participantReceiver{authority: authority, router: router, maxBytes: maxBytes, maxEntries: maxEntries, timeout: timeout, ctx: ctx, cancel: cancel, slots: make(chan struct{}, authority.maximum), refusals: make(chan struct{}, authority.maximum), done: make(chan struct{})}, nil
}
func (r *participantReceiver) Send(pkg *relay.Package) error {
	return r.SendContext(context.Background(), pkg)
}
func (r *participantReceiver) SendContext(ctx context.Context, pkg *relay.Package) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if pkg == nil || pkg.ReceivedFrom == "" || pkg.Source.Node != pkg.ReceivedFrom || pkg.Source.Host != RegistryHostID || pkg.Target.Node != r.authority.self || pkg.Target.Host != RegistryHostID {
		return errParticipantPeerMismatch
	}
	if len(pkg.Messages) != 1 || pkg.Messages[0] == nil || pkg.Messages[0].Topic != participantSnapshotRequestTopic || len(pkg.Messages[0].Payloads) != 1 || pkg.Messages[0].Payloads[0] == nil {
		return errParticipantWireInvalid
	}
	body, ok := pkg.Messages[0].Payloads[0].Data().([]byte)
	if !ok {
		return errParticipantWireInvalid
	}
	request, err := decodeParticipantRequest(body, r.maxBytes)
	if err != nil {
		return err
	}
	r.mu.Lock()
	if r.stopping || r.ctx.Err() != nil {
		r.mu.Unlock()
		return context.Canceled
	}
	select {
	case r.slots <- struct{}{}:
	default:
		select {
		case r.refusals <- struct{}{}:
			r.work.Add(1)
			r.mu.Unlock()
			go r.refuseBusy(pkg, request)
			return nil
		default:
			r.mu.Unlock()
			return errParticipantAuthorityBusy
		}
	}
	r.work.Add(1)
	r.mu.Unlock()
	go r.serve(pkg, request)
	return nil
}
func (r *participantReceiver) serve(pkg *relay.Package, request *participantWireRequest) {
	defer r.work.Done()
	defer func() { <-r.slots }()
	defer relay.ReleasePackage(pkg)
	ctx, cancel := context.WithTimeout(r.ctx, r.timeout)
	defer cancel()
	snapshot, err := r.authority.snapshotForPackageSince(ctx, pkg, request.Incarnation, request.Lifetime, request.KnownRevision)
	var body []byte
	topic := participantSnapshotResponseTopic
	if err == nil {
		response := &participantWireResponse{Version: participantWireVersion, Correlation: request.Correlation, Incarnation: request.Incarnation, Revision: snapshot.Revision, Lifetime: snapshot.Lifetime, Unchanged: snapshot.Unchanged, Entries: make([]kvapi.Entry, 0, len(snapshot.Entries))}
		for _, entry := range snapshot.Entries {
			response.Entries = append(response.Entries, entry)
		}
		body, err = encodeParticipantResponse(response, r.maxBytes, r.maxEntries)
	}
	if err != nil {
		topic = participantSnapshotFailureTopic
		var redirect *participantRedirectError
		if errors.As(err, &redirect) {
			topic = participantSnapshotRedirectTopic
			body, err = encodeParticipantRedirect(request.Correlation, request.Incarnation, redirect.Peer, r.maxBytes)
		} else {
			body, err = encodeParticipantFailure(request.Correlation, request.Incarnation, err, r.maxBytes)
		}
		if err != nil {
			return
		}
	}
	reply := relay.NewServicePackage(r.authority.self, RegistryHostID, pkg.ReceivedFrom, RegistryHostID, topic, payload.New(body))
	if err := r.router.SendContext(ctx, reply); err != nil {
		relay.ReleasePackage(reply)
	}
}

// refuseBusy owns an accepted request but does not enroll or capture. The
// refusal budget equals the configured capture budget; when both are full,
// admission still fails without retaining a packet or spawning a worker.
func (r *participantReceiver) refuseBusy(pkg *relay.Package, request *participantWireRequest) {
	defer r.work.Done()
	defer func() { <-r.refusals }()
	defer relay.ReleasePackage(pkg)
	ctx, cancel := context.WithTimeout(r.ctx, r.timeout)
	defer cancel()
	body, err := encodeParticipantFailure(request.Correlation, request.Incarnation, errParticipantAuthorityBusy, r.maxBytes)
	if err != nil {
		return
	}
	reply := relay.NewServicePackage(r.authority.self, RegistryHostID, pkg.ReceivedFrom, RegistryHostID, participantSnapshotFailureTopic, payload.New(body))
	if err := r.router.SendContext(ctx, reply); err != nil {
		relay.ReleasePackage(reply)
	}
}

func (r *participantReceiver) stop(ctx context.Context) error {
	r.stopOnce.Do(func() {
		r.mu.Lock()
		r.stopping = true
		r.cancel()
		r.mu.Unlock()
		go func() { r.work.Wait(); close(r.done) }()
	})
	if err := r.authority.stop(ctx); err != nil {
		return err
	}
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
