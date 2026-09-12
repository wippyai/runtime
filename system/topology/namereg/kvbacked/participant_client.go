// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

var participantCorrelations atomic.Uint64

type participantClientResult struct {
	snapshot *participantWireResponse
	err      error
}
type participantClientRequest struct {
	correlation uint64
	peer        pid.NodeID
	result      chan participantClientResult
}

// participantClient is a single-flight, context-aware authority snapshot source.
// Its incarnation must identify this naming lifetime and never be reused after
// retirement. Global correlations also distinguish attempts within this process.
type participantClient struct {
	self                 pid.NodeID
	incarnation          string
	resolve              func(context.Context) (pid.NodeID, error)
	router               relay.ContextSender
	maxBytes, maxEntries int
	maxParticipants      int
	timeout              time.Duration
	// Configured before startup; zero refuses redirects. All attempts share timeout.
	maxRedirects int
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	pending      *participantClientRequest
	cached       *participantWireResponse
	cachedPeer   pid.NodeID
	stopping     bool
	done         chan struct{}
}

func newParticipantClient(ctx context.Context, self pid.NodeID, incarnation string, resolve func(context.Context) (pid.NodeID, error), router relay.ContextSender, maxBytes, maxEntries int, timeout time.Duration) (*participantClient, error) {
	if err := validateParticipantIdentity(self, incarnation); err != nil {
		return nil, err
	}
	if resolve == nil || router == nil || maxBytes <= 0 || maxEntries <= 0 || timeout <= 0 {
		return nil, fmt.Errorf("invalid participant client configuration")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owned, cancel := context.WithCancel(ctx)
	return &participantClient{self: self, incarnation: incarnation, resolve: resolve, router: router, maxBytes: maxBytes, maxEntries: maxEntries, maxParticipants: maxBytes, timeout: timeout, ctx: owned, cancel: cancel, done: make(chan struct{})}, nil
}
func (c *participantClient) ScanAtIndex(prefix string, visit func(kvapi.Entry) bool) (uint64, error) {
	return c.ScanAtIndexContext(context.Background(), prefix, visit)
}
func (c *participantClient) ScanAtIndexContext(ctx context.Context, prefix string, visit func(kvapi.Entry) bool) (uint64, error) {
	if prefix != registryPrefix {
		return 0, fmt.Errorf("participant snapshot requires registry prefix")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	c.mu.Lock()
	if c.stopping || c.ctx.Err() != nil {
		c.mu.Unlock()
		return 0, context.Canceled
	}
	if c.pending != nil {
		c.mu.Unlock()
		return 0, errParticipantAuthorityBusy
	}
	request := &participantClientRequest{}
	c.pending = request
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.pending = nil
		if c.stopping {
			c.cached = nil
			c.cachedPeer = ""
			close(c.done)
		}
		c.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	stop := context.AfterFunc(c.ctx, cancel)
	defer func() { stop(); cancel() }()
	peer, err := c.resolve(ctx)
	if err != nil {
		return 0, err
	}
	if peer == "" {
		return 0, errParticipantUnavailable
	}
	seen := make(map[pid.NodeID]struct{})
	for redirects := 0; ; redirects++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		seen[peer] = struct{}{}
		c.mu.Lock()
		request.peer = peer
		request.correlation = participantCorrelations.Add(1)
		request.result = make(chan participantClientResult, 1)
		cached := c.cached
		if c.cachedPeer != peer {
			cached = nil
		}
		c.mu.Unlock()
		wireRequest := &participantWireRequest{Version: participantWireVersion, Correlation: request.correlation, Incarnation: c.incarnation}
		if cached != nil {
			wireRequest.Lifetime, wireRequest.KnownRevision = cached.Lifetime, cached.Revision
		}
		body, err := encodeParticipantRequest(wireRequest, c.maxBytes)
		if err != nil {
			return 0, err
		}
		pkg := relay.NewServicePackage(c.self, RegistryHostID, peer, RegistryHostID, participantSnapshotRequestTopic, payload.New(body))
		if err := c.router.SendContext(ctx, pkg); err != nil {
			relay.ReleasePackage(pkg)
			return 0, err
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case result := <-request.result:
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			if result.err != nil {
				var redirect *participantRedirectError
				if errors.As(result.err, &redirect) && redirects < c.maxRedirects {
					if _, repeated := seen[redirect.Peer]; repeated || redirect.Peer == c.self {
						return 0, errParticipantUnavailable
					}
					peer = redirect.Peer
					continue
				}
				return 0, result.err
			}
			snapshot := result.snapshot
			if cached != nil && snapshot.Lifetime == cached.Lifetime && snapshot.Revision < cached.Revision {
				return 0, errParticipantWireInvalid
			}
			if snapshot.Unchanged {
				if cached == nil || snapshot.Lifetime != cached.Lifetime || snapshot.Revision != cached.Revision {
					return 0, errParticipantWireInvalid
				}
				snapshot = cached
			}
			complete := true
			for _, entry := range snapshot.Entries {
				if err := ctx.Err(); err != nil {
					return 0, err
				}
				// Visitors own their bytes. Neither a full response nor cached replay
				// shares mutable value storage with the retained snapshot.
				entry.Value = bytes.Clone(entry.Value)
				if !visit(entry) {
					complete = false
					break
				}
			}
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			if complete && !result.snapshot.Unchanged && c.cacheableSnapshot(ctx, snapshot) {
				c.mu.Lock()
				if !c.stopping && ctx.Err() == nil {
					c.cached, c.cachedPeer = snapshot, peer
				}
				c.mu.Unlock()
			}
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			return snapshot.Revision, nil
		}
	}
}
func (c *participantClient) Send(pkg *relay.Package) error {
	return c.SendContext(context.Background(), pkg)
}
func (c *participantClient) SendContext(ctx context.Context, pkg *relay.Package) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	request := c.pending
	if c.stopping || request == nil || request.peer == "" || pkg == nil || pkg.ReceivedFrom != request.peer || pkg.Source.Node != request.peer || pkg.Source.Host != RegistryHostID || pkg.Target.Node != c.self || pkg.Target.Host != RegistryHostID {
		return errParticipantPeerMismatch
	}
	if len(pkg.Messages) != 1 || pkg.Messages[0] == nil || len(pkg.Messages[0].Payloads) != 1 || pkg.Messages[0].Payloads[0] == nil {
		return errParticipantWireInvalid
	}
	msg := pkg.Messages[0]
	body, ok := msg.Payloads[0].Data().([]byte)
	if !ok {
		return errParticipantWireInvalid
	}
	var result participantClientResult
	var correlation uint64
	var incarnation string
	switch msg.Topic {
	case participantSnapshotResponseTopic:
		response, err := decodeParticipantResponse(body, c.maxBytes, c.maxEntries)
		if err != nil {
			return err
		}
		result.snapshot = response
		correlation, incarnation = response.Correlation, response.Incarnation
	case participantSnapshotRedirectTopic:
		var peer pid.NodeID
		var err error
		correlation, incarnation, peer, err = decodeParticipantRedirect(body, c.maxBytes)
		if err != nil {
			return err
		}
		result.err = &participantRedirectError{Peer: peer}
	case participantSnapshotFailureTopic:
		correlation, incarnation, result.err = decodeParticipantFailure(body, c.maxBytes)
		if correlation == 0 {
			return result.err
		}
	default:
		return errParticipantWireInvalid
	}
	// A valid stale response is consumed but cannot complete the current request.
	if correlation == request.correlation && incarnation == c.incarnation {
		select {
		case request.result <- result:
		default:
		}
	}
	relay.ReleasePackage(pkg)
	return nil
}
func (c *participantClient) stop(ctx context.Context) error {
	c.mu.Lock()
	if !c.stopping {
		c.stopping = true
		c.cancel()
		if c.pending == nil {
			c.cached = nil
			c.cachedPeer = ""
			close(c.done)
		}
	}
	c.mu.Unlock()
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *participantClient) participantIdentity() (pid.NodeID, string) { return c.self, c.incarnation }
