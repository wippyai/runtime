// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type heldFirstSnapshot struct {
	kvapi.Engine
	reads    atomic.Uint64
	captured chan struct{}
	release  chan struct{}
}

func (e *heldFirstSnapshot) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	entries, index, err := e.Engine.(kvapi.LocalSnapshotReader).ReadLocalSnapshot(keys)
	if e.reads.Add(1) == 1 {
		close(e.captured)
		<-e.release
	}
	return entries, index, err
}

func TestDelayedActiveSnapshotCannotResurrectDeletedClaim(t *testing.T) {
	r := newStrongReg(t, nil, 0, nil)
	owner := mkPID("node-1", "owner")
	value, err := encode(activeValue{Name: "name", PID: owner.String(), Strong: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(activeKey("name"), value); err != nil {
		t.Fatal(err)
	}
	engine := &heldFirstSnapshot{Engine: r.engine, captured: make(chan struct{}), release: make(chan struct{})}
	r.engine = engine
	var once sync.Once
	release := func() { once.Do(func() { close(engine.release) }) }
	defer release()
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { first <- r.strong.reconcile("name") }()
	<-engine.captured
	independent := make(chan error, 1)
	go func() { independent <- r.strong.reconcile("other") }()
	select {
	case err := <-independent:
		if err != nil {
			release()
			<-first
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		release()
		<-first
		<-independent
		t.Fatal("a stalled name blocked an unrelated name")
	}
	if err := engine.Engine.Delete(activeKey("name")); err != nil {
		release()
		<-first
		t.Fatal(err)
	}
	go func() { second <- r.strong.reconcile("name") }()
	// The old implementation can apply the deletion while the older active
	// snapshot is held. Ordered reconciliation instead waits for that snapshot.
	secondFinished := false
	select {
	case err := <-second:
		secondFinished = true
		if err != nil {
			release()
			<-first
			t.Fatal(err)
		}
	case <-time.After(50 * time.Millisecond):
	}
	release()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if !secondFinished {
		if err := <-second; err != nil {
			t.Fatal(err)
		}
	}
	if _, held := r.IsStrongReserved("name"); held {
		t.Fatal("delayed active snapshot resurrected deleted claim")
	}
}
