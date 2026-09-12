// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"strings"
	"testing"
	"time"
)

func TestStopJoinsPendingRegistrationWithoutReleasingClaim(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "peer"}, time.Hour, nil)
	if err := r.StartReconciler(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer r.StopReconciler(context.Background())
	owner := mkPID("node-1", "owner")
	result := make(chan error, 1)
	go func() {
		_, err := r.RegisterScope(context.Background(), "name", owner, globalapi.Strong)
		result <- err
	}()
	if !eventually(t, time.Second, func() bool { _, held := r.IsStrongReserved("name"); return held }) {
		t.Fatal("registration never reached pending")
	}
	if err := r.StopReconciler(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("registration result=%v", err)
	}
	if _, err := r.engine.Get(pendingKey("name")); err != nil {
		t.Fatalf("shutdown removed authoritative pending claim: %v", err)
	}
	for _, scope := range []globalapi.RegistrationMode{globalapi.Strong, globalapi.Consistent} {
		if _, err := r.RegisterScope(context.Background(), "after-stop", owner, scope); !errors.Is(err, context.Canceled) {
			t.Fatalf("scope=%v admitted after stop: %v", scope, err)
		}
	}
}

type failedAckEngine struct {
	kvapi.Engine
	failure error
}

func (e failedAckEngine) SetIfAbsent(key string, value []byte) (kvapi.Version, bool, error) {
	if strings.HasPrefix(key, ackPrefix) {
		return 0, false, e.failure
	}
	return e.Engine.SetIfAbsent(key, value)
}
func TestRegistrationReturnsAttestationFailureWithoutErasingPending(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Hour, nil)
	failure := errors.New("injected acknowledgement failure")
	r.engine = failedAckEngine{r.engine, failure}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := r.RegisterScope(ctx, "name", mkPID("node-1", "owner"), globalapi.Strong)
	if !errors.Is(err, failure) {
		t.Fatalf("attestation error lost: %v", err)
	}
	if _, err := r.engine.Get(pendingKey("name")); err != nil {
		t.Fatalf("uncertain claim erased: %v", err)
	}
}

type heldRegistrationTxn struct {
	kvapi.Engine
	entered, release chan struct{}
}

func (e *heldRegistrationTxn) Txn(ops []kvapi.TxnOp) (bool, error) {
	close(e.entered)
	<-e.release
	return e.Engine.Txn(ops)
}
func (e *heldRegistrationTxn) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	return e.Engine.(kvapi.LocalSnapshotReader).ReadLocalSnapshot(keys)
}
func TestStopWaitsForAdmittedRegistrationTransaction(t *testing.T) {
	r := newStrongReg(t, nil, 0, nil)
	engine := &heldRegistrationTxn{Engine: r.engine, entered: make(chan struct{}), release: make(chan struct{})}
	r.engine = engine
	if err := r.StartReconciler(context.Background()); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := r.RegisterScope(context.Background(), "name", mkPID("node-1", "owner"), globalapi.Consistent)
		result <- err
	}()
	<-engine.entered
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := r.StopReconciler(ctx); !errors.Is(err, context.DeadlineExceeded) {
		close(engine.release)
		<-result
		_ = r.StopReconciler(context.Background())
		t.Fatalf("stop returned before admitted transaction: %v", err)
	}
	close(engine.release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := r.StopReconciler(context.Background()); err != nil {
		t.Fatal(err)
	}
}
