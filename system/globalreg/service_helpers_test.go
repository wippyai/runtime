// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"context"
	"sync/atomic"
	"time"

	hraft "github.com/hashicorp/raft"

	"github.com/wippyai/runtime/api/event"
	raftapi "github.com/wippyai/runtime/api/raft"
	"github.com/wippyai/runtime/api/relay"

	"go.uber.org/zap"
)

// directApplyRaft is a test stub that satisfies raftapi.Service by
// invoking FSM.Apply synchronously. Useful for service-level tests that
// don't exercise Raft replication itself but need a real Apply pipeline.
type directApplyRaft struct {
	fsm         *FSM
	leaderCh    chan bool
	knownLeader raftapi.ServerID
	idx         atomic.Uint64
	leader      atomic.Bool
}

func newDirectApplyRaft(fsm *FSM, leader bool) *directApplyRaft {
	r := &directApplyRaft{fsm: fsm, leaderCh: make(chan bool, 1)}
	r.leader.Store(leader)
	return r
}

func (r *directApplyRaft) Apply(data []byte, _ time.Duration) (*raftapi.ApplyResponse, error) {
	if !r.leader.Load() {
		return nil, raftapi.ErrNotLeader
	}
	idx := r.idx.Add(1)
	resp := r.fsm.Apply(&hraft.Log{Data: data, Index: idx})
	return &raftapi.ApplyResponse{Response: resp, Index: idx}, nil
}

func (r *directApplyRaft) Leader() (raftapi.ServerID, raftapi.ServerAddress, error) {
	if r.leader.Load() {
		return "local", "local:0", nil
	}
	if r.knownLeader != "" {
		return r.knownLeader, r.knownLeader + ":0", nil
	}
	return "", "", nil
}

func (r *directApplyRaft) IsLeader() bool        { return r.leader.Load() }
func (r *directApplyRaft) LeaderCh() <-chan bool { return r.leaderCh }
func (r *directApplyRaft) State() raftapi.State  { return raftapi.Leader }
func (r *directApplyRaft) Barrier(_ time.Duration) error {
	return nil
}
func (r *directApplyRaft) CommitIndex() uint64 { return r.idx.Load() }
func (r *directApplyRaft) AddVoter(_ raftapi.ServerID, _ raftapi.ServerAddress, _ time.Duration) error {
	return nil
}
func (r *directApplyRaft) AddNonvoter(_ raftapi.ServerID, _ raftapi.ServerAddress, _ time.Duration) error {
	return nil
}
func (r *directApplyRaft) DemoteVoter(_ raftapi.ServerID, _ time.Duration) error {
	return nil
}
func (r *directApplyRaft) RemoveServer(_ raftapi.ServerID, _ time.Duration) error {
	return nil
}
func (r *directApplyRaft) LeadershipTransfer(_ raftapi.ServerID, _ time.Duration) error {
	return nil
}
func (r *directApplyRaft) GetConfiguration() ([]raftapi.Server, error) {
	return nil, nil
}
func (r *directApplyRaft) Stats() map[string]string { return nil }
func (r *directApplyRaft) LastContact() time.Time   { return time.Time{} }

// nopBus is a no-op event.Bus that ignores subscriptions.
type nopBus struct{}

func (b *nopBus) SubscribeP(_ context.Context, _ event.System, _ event.Kind, _ chan<- event.Event) (event.SubscriberID, error) {
	return event.SubscriberID(""), nil
}
func (b *nopBus) Subscribe(_ context.Context, _ event.System, _ chan<- event.Event) (event.SubscriberID, error) {
	return event.SubscriberID(""), nil
}
func (b *nopBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {}
func (b *nopBus) Send(_ context.Context, _ event.Event)               {}

// nopRouter is a no-op relay.Receiver/Sender used to satisfy NewService.
type nopRouter struct{}

func (r *nopRouter) Send(pkg *relay.Package) error {
	relay.ReleasePackage(pkg)
	return nil
}

func noopLogger() *zap.Logger {
	return zap.NewNop()
}
