// SPDX-License-Identifier: MPL-2.0

package eventualreg_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/eventualreg"
)

// recordingReceiver captures every relay package the service emits so a test
// can assert name_revoked delivery to the correct local PID.
type recordingReceiver struct {
	mu   sync.Mutex
	pkgs []*relay.Package
}

func (r *recordingReceiver) Send(p *relay.Package) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pkgs = append(r.pkgs, p)
	return nil
}

func (r *recordingReceiver) revokes() []topology.NameRevokedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []topology.NameRevokedEvent
	for _, pkg := range r.pkgs {
		for _, m := range pkg.Messages {
			if m.Topic != topology.TopicEvents {
				continue
			}
			for _, pl := range m.Payloads {
				if ev, ok := pl.Data().(*topology.NameRevokedEvent); ok {
					out = append(out, *ev)
				}
			}
		}
	}
	return out
}

// localLiveEntryForName mints a remote delta for `name` from origin `originID`
// so a test can drive a cross-origin conflict against the local node.
func remoteDelta(name string, p pid.PID, origin string, counter uint64, priority uint32) ([]byte, error) {
	e := &eventualreg.Entry{
		Name:     name,
		PID:      p,
		Counter:  counter,
		Wall:     1000,
		Priority: priority,
	}
	return eventualreg.EncodeDelta(nil, e, origin)
}

// frameOf wraps a single delta body into a FrameTypeDelta frame for OnFrame.
func frameOf(t *testing.T, body []byte) []byte {
	t.Helper()
	return append([]byte{byte(eventualreg.FrameTypeDelta)}, body...)
}

// TestRevoke_OnIncomingConflict proves the home node signals name_revoked when
// a local live registration loses to a higher-priority different-origin entry
// arriving via gossip, carrying the exact {Name, PID} that was lost.
func TestRevoke_OnIncomingConflict(t *testing.T) {
	rec := &recordingReceiver{}
	svc := eventualreg.NewService(eventualreg.Config{
		LocalNodeID: "node-A",
		Revoker:     rec,
	})
	require.NoError(t, svc.Start(context.Background()))
	defer svc.Stop()

	localPID := pid.PID{Node: "node-A", Host: "h", UniqID: "local"}
	_, err := svc.Register("svc.leader", localPID)
	require.NoError(t, err)

	// A different origin claims the same name with higher priority — it must win
	// the concurrent conflict, evicting the local live binding.
	winnerPID := pid.PID{Node: "node-B", Host: "h", UniqID: "remote"}
	body, err := remoteDelta("svc.leader", winnerPID, "node-B", 1, 10)
	require.NoError(t, err)
	svc.OnFrame(frameOf(t, body))

	// Local lookup now resolves to the cross-origin winner.
	res, err := svc.Lookup(context.Background(), "svc.leader")
	require.NoError(t, err)
	require.True(t, res.Found)
	assert.Equal(t, winnerPID, res.PID)

	// The local process that lost must have received exactly one revoke for it.
	revokes := rec.revokes()
	require.Len(t, revokes, 1)
	assert.Equal(t, "svc.leader", revokes[0].Name)
	assert.Equal(t, localPID, revokes[0].PID)
	assert.Equal(t, topology.NameRevoked, revokes[0].Kind)
}

// TestRevoke_DedupeOnReapply proves anti-entropy re-applying the same winner
// does not re-fire the revoke: it is emitted once per lost dot.
func TestRevoke_DedupeOnReapply(t *testing.T) {
	rec := &recordingReceiver{}
	svc := eventualreg.NewService(eventualreg.Config{LocalNodeID: "node-A", Revoker: rec})
	require.NoError(t, svc.Start(context.Background()))
	defer svc.Stop()

	localPID := pid.PID{Node: "node-A", Host: "h", UniqID: "local"}
	_, err := svc.Register("svc.leader", localPID)
	require.NoError(t, err)

	winnerPID := pid.PID{Node: "node-B", Host: "h", UniqID: "remote"}
	body, err := remoteDelta("svc.leader", winnerPID, "node-B", 1, 10)
	require.NoError(t, err)

	// Apply the same winning delta three times (gossip + anti-entropy replays).
	svc.OnFrame(frameOf(t, body))
	svc.OnFrame(frameOf(t, body))
	svc.OnFrame(frameOf(t, body))

	assert.Len(t, rec.revokes(), 1, "revoke must fire once per lost dot")
}

// TestRevoke_OnImmediateRegisterLoss proves the Register path itself signals a
// revoke when a fresh local registration immediately loses to an existing
// higher-priority different-origin entry, and returns a not-won error.
func TestRevoke_OnImmediateRegisterLoss(t *testing.T) {
	rec := &recordingReceiver{}
	svc := eventualreg.NewService(eventualreg.Config{LocalNodeID: "node-A", Revoker: rec})
	require.NoError(t, svc.Start(context.Background()))
	defer svc.Stop()

	// A higher-priority remote claim lands first.
	winnerPID := pid.PID{Node: "node-B", Host: "h", UniqID: "remote"}
	body, err := remoteDelta("svc.leader", winnerPID, "node-B", 1, 10)
	require.NoError(t, err)
	svc.OnFrame(frameOf(t, body))

	// Now a local registration of the same name must lose immediately.
	localPID := pid.PID{Node: "node-A", Host: "h", UniqID: "local"}
	got, err := svc.Register("svc.leader", localPID)
	require.Error(t, err)
	assert.Equal(t, eventualreg.ErrNameAlreadyRegistered, err)
	assert.Equal(t, winnerPID, got, "Register returns the cross-origin winner")

	revokes := rec.revokes()
	require.Len(t, revokes, 1)
	assert.Equal(t, localPID, revokes[0].PID)
	assert.Equal(t, "svc.leader", revokes[0].Name)
}

// TestRevoke_NotEmittedWhenLocalWins proves no revoke fires when the local
// registration wins the concurrent conflict.
func TestRevoke_NotEmittedWhenLocalWins(t *testing.T) {
	rec := &recordingReceiver{}
	svc := eventualreg.NewService(eventualreg.Config{LocalNodeID: "node-A", Revoker: rec})
	require.NoError(t, svc.Start(context.Background()))
	defer svc.Stop()

	localPID := pid.PID{Node: "node-A", Host: "h", UniqID: "local"}
	// Local registers with high priority — it must beat a lower-priority remote.
	_, err := svc.Register("svc.leader", localPID, eventualreg.WithPriority(10))
	require.NoError(t, err)

	loserPID := pid.PID{Node: "node-B", Host: "h", UniqID: "remote"}
	body, err := remoteDelta("svc.leader", loserPID, "node-B", 1, 0)
	require.NoError(t, err)
	svc.OnFrame(frameOf(t, body))

	res, err := svc.Lookup(context.Background(), "svc.leader")
	require.NoError(t, err)
	require.True(t, res.Found)
	assert.Equal(t, localPID, res.PID, "local high-priority registration wins")
	assert.Empty(t, rec.revokes(), "winner must not be revoked")
}
