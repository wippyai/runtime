// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

const testStrongAttemptID = "00000000000000000000000000000001"

func nextResultEvent(t *testing.T, w kvapi.Watcher) kvapi.WatchEvent {
	t.Helper()
	select {
	case <-w.Done():
		t.Fatalf("result watch invalid: %v", w.Err())
	case ev, ok := <-w.Events():
		if !ok {
			t.Fatal("result watch closed before event")
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("result watch event missing")
	}
	return kvapi.WatchEvent{}
}

func TestStrongTerminalResultWatchCarriesCommittedAttemptEvidence(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "peer"}, time.Second, nil)
	owner := mkPID("node-1", "owner")
	hdr := pendingHeader{
		PID:           owner.String(),
		Name:          "claim:with:colons",
		AttemptID:     testStrongAttemptID,
		RequiredNodes: []pid.NodeID{"node-1", "peer"},
	}
	value, err := encode(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(pendingKey(hdr.Name), value); err != nil {
		t.Fatal(err)
	}
	pending, err := r.engine.Get(pendingKey(hdr.Name))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.engine.SetIfAbsent(ackKey(hdr.Name, hdr.AttemptID, "node-1"), []byte("node-1")); err != nil {
		t.Fatal(err)
	}

	w, err := r.engine.Watch(context.Background(), resultPrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()

	r.strong.leaderExpire(hdr.Name, pending.Epoch, pending.Version, hdr, "deadline")

	put := nextResultEvent(t, w)
	if put.Current == nil || put.Type != kvapi.WatchPut {
		t.Fatalf("first terminal event = %+v, want result put", put)
	}
	got, err := decodeTerminalResult(put.Current.Value)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != hdr.Name || got.AttemptID != hdr.AttemptID || got.Reason != "deadline" || got.Epoch != pending.Epoch {
		t.Fatalf("terminal result = %+v, want name=%q attempt=%q reason=deadline epoch=%d", got, hdr.Name, hdr.AttemptID, pending.Epoch)
	}
	if len(got.Missing) != 1 || got.Missing[0] != "peer" {
		t.Fatalf("terminal result missing = %v, want [peer]", got.Missing)
	}
	if _, err := r.engine.Get(resultKey(hdr.Name, hdr.AttemptID)); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("terminal result key remained after committed txn: %v", err)
	}

	del := nextResultEvent(t, w)
	if del.Type != kvapi.WatchDelete || del.Current != nil || del.Previous == nil {
		t.Fatalf("second terminal event = %+v, want result delete", del)
	}
	previous, err := decodeTerminalResult(del.Previous.Value)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(previous, got) {
		t.Fatalf("delete previous result = %+v, put result = %+v", previous, got)
	}
	if _, err := r.engine.Get(pendingKey(hdr.Name)); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("pending key remained after committed terminal txn: %v", err)
	}
}

func TestStrongTerminalResultFailedTxnEmitsNothing(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "peer"}, time.Second, nil)
	owner := mkPID("node-1", "owner")
	hdr := pendingHeader{
		PID:           owner.String(),
		Name:          "claim",
		AttemptID:     testStrongAttemptID,
		RequiredNodes: []pid.NodeID{"node-1", "peer"},
	}
	value, err := encode(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(pendingKey(hdr.Name), value); err != nil {
		t.Fatal(err)
	}
	pending, err := r.engine.Get(pendingKey(hdr.Name))
	if err != nil {
		t.Fatal(err)
	}
	oldResult, err := encode(terminalResult{
		Name:      hdr.Name,
		AttemptID: hdr.AttemptID,
		Reason:    "old",
		Epoch:     pending.Epoch,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(resultKey(hdr.Name, hdr.AttemptID), oldResult); err != nil {
		t.Fatal(err)
	}
	w, err := r.engine.Watch(context.Background(), resultPrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()

	r.strong.leaderExpire(hdr.Name, pending.Epoch, pending.Version, hdr, "deadline")

	select {
	case ev := <-w.Events():
		t.Fatalf("failed terminal txn emitted event: %+v", ev)
	case <-w.Done():
		t.Fatalf("result watch invalid: %v", w.Err())
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := r.engine.Get(pendingKey(hdr.Name)); err != nil {
		t.Fatalf("failed terminal txn removed pending key: %v", err)
	}
	current, err := r.engine.Get(resultKey(hdr.Name, hdr.AttemptID))
	if err != nil {
		t.Fatal(err)
	}
	if string(current.Value) != string(oldResult) {
		t.Fatalf("failed terminal txn changed existing result: got %x want %x", current.Value, oldResult)
	}
}
