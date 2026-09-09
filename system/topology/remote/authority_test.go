// SPDX-License-Identifier: MPL-2.0

package remote

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

const (
	localNode = pid.NodeID("local")
	peerNode  = pid.NodeID("peer")
)

func monitorSpec(expires time.Time) GrantSpec {
	return GrantSpec{
		PeerNode: peerNode,
		Watcher:  pid.PID{Node: peerNode, Host: "client", UniqID: "watcher-1"},
		Target:   pid.PID{Node: localNode, Host: "owner", UniqID: "target-1"},
		Expires:  expires,
	}
}

func liveIngress(connection <-chan struct{}) relay.IngressIdentity {
	return relay.IngressIdentity{
		Node:               peerNode,
		Authenticated:      true,
		IntegrityProtected: true,
		ConnectionClosed:   connection,
	}
}

func mintGrant(t *testing.T, a *Authority) (Token, GrantSpec) {
	t.Helper()
	spec := monitorSpec(time.Now().Add(time.Minute))
	token, err := a.Grant(spec)
	require.NoError(t, err)
	return token, spec
}

func acquire(t *testing.T, a *Authority, token Token, spec GrantSpec, connection <-chan struct{}) *Lease {
	t.Helper()
	lease, err := a.Acquire(token, liveIngress(connection), spec.Watcher, spec.Target)
	require.NoError(t, err)
	return lease
}

func TestAuthorityRejectsWrongTupleAndStalePIDCache(t *testing.T) {
	a, err := NewAuthority(localNode, 2)
	require.NoError(t, err)
	token, spec := mintGrant(t, a)
	connection := make(chan struct{})

	wrongWatcher := spec.Watcher
	wrongWatcher.UniqID = "other"
	_, err = a.Acquire(token, liveIngress(connection), wrongWatcher, spec.Target)
	require.ErrorIs(t, err, ErrGrantDenied)

	wrongTarget := spec.Target
	wrongTarget.Host = "other-owner"
	_, err = a.Acquire(token, liveIngress(connection), spec.Watcher, wrongTarget)
	require.ErrorIs(t, err, ErrGrantDenied)

	// PID.String may retain a cached value after mutation. Authorization must use
	// the fields, so this stale cache cannot make the changed actor match.
	stale := spec.Watcher.Precomputed()
	stale.UniqID = "other"
	_, err = a.Acquire(token, liveIngress(connection), stale, spec.Target)
	require.ErrorIs(t, err, ErrGrantDenied)
}

func TestTokenUsesCanonicalOpaqueWireEncoding(t *testing.T) {
	a, err := NewAuthority(localNode, 1)
	require.NoError(t, err)
	token, _ := mintGrant(t, a)
	encoded := token.String()
	require.Len(t, encoded, 64)
	decoded, err := ParseToken(encoded)
	require.NoError(t, err)
	require.Equal(t, token, decoded)
	_, err = ParseToken(strings.ToUpper(encoded))
	require.ErrorIs(t, err, ErrGrantDenied)
}

func TestAuthorityRejectsInvalidSpecAndIngress(t *testing.T) {
	a, err := NewAuthority(localNode, 1)
	require.NoError(t, err)

	bad := monitorSpec(time.Now().Add(time.Minute))
	bad.Watcher.Host = "client|ambiguous"
	_, err = a.Grant(bad)
	require.ErrorIs(t, err, ErrInvalidGrantSpec)

	token, spec := mintGrant(t, a)
	connection := make(chan struct{})
	plain := liveIngress(connection)
	plain.IntegrityProtected = false
	_, err = a.Acquire(token, plain, spec.Watcher, spec.Target)
	require.ErrorIs(t, err, ErrIngressUnauthorized)

	_, err = a.Acquire(token, liveIngress(nil), spec.Watcher, spec.Target)
	require.ErrorIs(t, err, ErrIngressClosed)

	closed := make(chan struct{})
	close(closed)
	_, err = a.Acquire(token, liveIngress(closed), spec.Watcher, spec.Target)
	require.ErrorIs(t, err, ErrIngressClosed)

	wrongPeer := liveIngress(connection)
	wrongPeer.Node = "other-peer"
	_, err = a.Acquire(token, wrongPeer, spec.Watcher, spec.Target)
	require.ErrorIs(t, err, ErrGrantDenied)
}

func TestAuthorityExpiryReclaimsCapacityAndClosesLease(t *testing.T) {
	a, err := NewAuthority(localNode, 1)
	require.NoError(t, err)
	spec := monitorSpec(time.Now().Add(20 * time.Millisecond))
	token, err := a.Grant(spec)
	require.NoError(t, err)
	connection := make(chan struct{})
	lease := acquire(t, a, token, spec, connection)

	select {
	case <-lease.Done():
	case <-time.After(time.Second):
		t.Fatal("expired grant did not notify lease cleanup")
	}
	require.ErrorIs(t, lease.Use(func() error { return nil }), ErrGrantExpired)
	_, err = a.Grant(monitorSpec(time.Now().Add(time.Minute)))
	require.NoError(t, err, "expired grant must reclaim bounded capacity")
}

func TestAuthorityRevokeAndConnectionCloseFenceStaleLeases(t *testing.T) {
	a, err := NewAuthority(localNode, 2)
	require.NoError(t, err)
	token, spec := mintGrant(t, a)
	connection := make(chan struct{})
	lease := acquire(t, a, token, spec, connection)
	require.True(t, a.Revoke(token))
	select {
	case <-lease.Done():
	default:
		t.Fatal("revoke did not close the lease")
	}
	require.ErrorIs(t, lease.Use(func() error { return nil }), ErrGrantRevoked)
	require.False(t, a.Revoke(token), "stale token cannot revoke a reclaimed entry")

	token, spec = mintGrant(t, a)
	connection = make(chan struct{})
	lease = acquire(t, a, token, spec, connection)
	close(connection)
	select {
	case <-lease.Done():
	case <-time.After(time.Second):
		t.Fatal("connection closure did not revoke the bound grant")
	}
	require.ErrorIs(t, lease.Use(func() error { return nil }), ErrIngressClosed)
}

func TestAuthorityDonePublishesOnlyAfterReclaimingCapacity(t *testing.T) {
	a, err := NewAuthority(localNode, 1)
	require.NoError(t, err)
	token, spec := mintGrant(t, a)
	lease := acquire(t, a, token, spec, make(chan struct{}))

	grantAfterDone := make(chan error, 1)
	go func() {
		<-lease.Done()
		_, err := a.Grant(monitorSpec(time.Now().Add(time.Minute)))
		grantAfterDone <- err
	}()
	require.True(t, a.Revoke(token))
	select {
	case err := <-grantAfterDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Done observer did not see reclaimed grant capacity")
	}
}

func TestAuthorityRejectsReconnectButAllowsSameConnectionRetry(t *testing.T) {
	a, err := NewAuthority(localNode, 1)
	require.NoError(t, err)
	token, spec := mintGrant(t, a)
	first := make(chan struct{})
	firstLease := acquire(t, a, token, spec, first)
	secondLease := acquire(t, a, token, spec, first)
	require.NoError(t, firstLease.Use(func() error { return nil }))
	require.NoError(t, secondLease.Use(func() error { return nil }))

	reconnected := make(chan struct{})
	_, err = a.Acquire(token, liveIngress(reconnected), spec.Watcher, spec.Target)
	require.ErrorIs(t, err, ErrIngressConnectionChanged)
}

func TestAuthorityUseAndRevokeAreFenced(t *testing.T) {
	a, err := NewAuthority(localNode, 1)
	require.NoError(t, err)
	token, spec := mintGrant(t, a)
	connection := make(chan struct{})
	lease := acquire(t, a, token, spec, connection)

	entered := make(chan struct{})
	allowReturn := make(chan struct{})
	useDone := make(chan error, 1)
	go func() {
		useDone <- lease.Use(func() error {
			close(entered)
			<-allowReturn
			return nil
		})
	}()
	<-entered

	revokeDone := make(chan bool, 1)
	go func() { revokeDone <- a.Revoke(token) }()
	select {
	case <-revokeDone:
		t.Fatal("revoke completed while an admitted Use callback was still running")
	case <-time.After(20 * time.Millisecond):
	}
	close(allowReturn)
	require.NoError(t, <-useDone)
	require.True(t, <-revokeDone)
	require.ErrorIs(t, lease.Use(func() error { return nil }), ErrGrantRevoked)
}

func TestAuthorityConcurrentUseAndRevokeNeverStartsAfterRevocation(t *testing.T) {
	a, err := NewAuthority(localNode, 1)
	require.NoError(t, err)
	token, spec := mintGrant(t, a)
	connection := make(chan struct{})
	lease := acquire(t, a, token, spec, connection)

	var started atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for range 16 {
		wg.Go(func() {
			errs <- lease.Use(func() error {
				started.Add(1)
				return nil
			})
		})
	}
	require.True(t, a.Revoke(token))
	wg.Wait()
	close(errs)
	for err := range errs {
		require.True(t, err == nil || errors.Is(err, ErrGrantRevoked))
	}
	before := started.Load()
	require.ErrorIs(t, lease.Use(func() error {
		started.Add(1)
		return nil
	}), ErrGrantRevoked)
	require.Equal(t, before, started.Load())
}

func TestAuthorityCloseFencesNewAdmissionsAndAllCallersWait(t *testing.T) {
	a, err := NewAuthority(localNode, 1)
	require.NoError(t, err)
	token, spec := mintGrant(t, a)
	connection := make(chan struct{})
	lease := acquire(t, a, token, spec, connection)

	entered := make(chan struct{})
	allowReturn := make(chan struct{})
	useDone := make(chan error, 1)
	go func() {
		useDone <- lease.Use(func() error {
			close(entered)
			<-allowReturn
			return nil
		})
	}()
	<-entered

	firstClose := make(chan struct{})
	secondClose := make(chan struct{})
	go func() { a.Close(); close(firstClose) }()
	select {
	case <-a.closing:
	case <-time.After(time.Second):
		t.Fatal("close did not start")
	}
	go func() { a.Close(); close(secondClose) }()

	_, err = a.Acquire(token, liveIngress(connection), spec.Watcher, spec.Target)
	require.ErrorIs(t, err, ErrAuthorityClosed)
	require.ErrorIs(t, lease.Use(func() error { return nil }), ErrAuthorityClosed)
	select {
	case <-firstClose:
		t.Fatal("close returned while an admitted callback was running")
	case <-secondClose:
		t.Fatal("a concurrent close returned before the initiating close completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(allowReturn)
	require.NoError(t, <-useDone)
	select {
	case <-firstClose:
	case <-time.After(time.Second):
		t.Fatal("initiating close did not complete")
	}
	select {
	case <-secondClose:
	case <-time.After(time.Second):
		t.Fatal("concurrent close did not wait for shared completion")
	}
}

func TestAuthorityUsePanicReleasesAdmissionGate(t *testing.T) {
	a, err := NewAuthority(localNode, 1)
	require.NoError(t, err)
	token, spec := mintGrant(t, a)
	lease := acquire(t, a, token, spec, make(chan struct{}))

	func() {
		defer func() { require.Equal(t, "boom", recover()) }()
		_ = lease.Use(func() error { panic("boom") })
	}()
	require.True(t, a.Revoke(token), "panic must not strand the revocation gate")
}

// A second retirement must join the first even after the admission gate has
// closed, while map reclamation and the cleanup notification are still pending.
func TestConcurrentRetirementWaitsForCleanupSignal(t *testing.T) {
	a, err := NewAuthority(localNode, 1)
	require.NoError(t, err)
	token, _ := mintGrant(t, a)
	a.mu.Lock()
	g := a.grants[token]
	first := make(chan bool, 1)
	go func() { first <- a.deactivate(g, ErrGrantRevoked) }()
	// Hold map reclamation so the intermediate inactive state is observable.
	deadline := time.Now().Add(time.Second)
	inactive := false
	for time.Now().Before(deadline) {
		g.gate.RLock()
		inactive = !g.active
		g.gate.RUnlock()
		if inactive {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !inactive {
		a.mu.Unlock()
		t.Fatal("first retirement did not close admission")
	}
	second := make(chan bool, 1)
	go func() { second <- a.deactivate(g, ErrAuthorityClosed) }()
	select {
	case <-second:
		a.mu.Unlock()
		t.Fatal("concurrent retirement returned before cleanup")
	case <-time.After(20 * time.Millisecond):
	}
	a.mu.Unlock()
	require.True(t, <-first)
	require.False(t, <-second)
	select {
	case <-g.done:
	default:
		t.Fatal("retirement returned before Done")
	}
	a.Close()
}
