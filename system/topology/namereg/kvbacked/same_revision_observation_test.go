// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	systemkv "github.com/wippyai/runtime/system/kv"
)

// sameRevisionSnapshotEngine replays one immutable publication captured from
// the real standalone KV engine. Reconciliation callbacks for different names
// can legitimately consume that same publication, even though the callbacks
// run one after another.
type sameRevisionSnapshotEngine struct {
	kvapi.Engine
	entries  map[string]kvapi.Entry
	revision uint64
}

func (e *sameRevisionSnapshotEngine) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	entries := make(map[string]kvapi.Entry, len(keys))
	for _, key := range keys {
		if entry, ok := e.entries[key]; ok {
			entry.Value = append([]byte(nil), entry.Value...)
			entries[key] = entry
		}
	}
	return entries, e.revision, nil
}

func TestReconcileSamePublicationDoesNotRetireAnotherName(t *testing.T) {
	for _, first := range []string{"absent", "pending"} {
		t.Run("first_"+first, func(t *testing.T) {
			base := systemkv.NewService("same-publication-"+first, nil)
			if _, err := base.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = base.Stop(context.Background()) })

			owner := mkPID("node-1", "owner")
			pending := putPendingSnapshotRecord(t, base, "pending", owner, []pid.NodeID{"node-1"})
			if pending.Epoch != 0 {
				t.Fatalf("standalone pending record has non-zero epoch: %d", pending.Epoch)
			}
			keys := []string{
				activeKey("absent"), pendingKey("absent"),
				activeKey("pending"), pendingKey("pending"),
			}
			entries, revision, err := base.ReadLocalSnapshot(keys)
			if err != nil {
				t.Fatal(err)
			}
			if revision == 0 {
				t.Fatal("standalone snapshot did not publish a revision")
			}
			if _, ok := entries[activeKey("absent")]; ok {
				t.Fatal("absent name unexpectedly has an active record")
			}
			if _, ok := entries[pendingKey("absent")]; ok {
				t.Fatal("absent name unexpectedly has a pending record")
			}
			if _, ok := entries[pendingKey("pending")]; !ok {
				t.Fatal("pending name missing from immutable publication")
			}

			engine := &sameRevisionSnapshotEngine{Engine: base, entries: entries, revision: revision}
			r := NewService(engine, "node-1", nil, nil)
			r.ConfigureStrong(StrongDeps{
				Membership: func() []pid.NodeID { return []pid.NodeID{"node-1"} },
				IsLeader:   func() bool { return false },
				Deadline:   time.Minute,
			})

			if first == "absent" {
				if err := r.strong.reconcile("absent"); err != nil {
					t.Fatal(err)
				}
				if err := r.strong.reconcile("pending"); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := r.strong.reconcile("pending"); err != nil {
					t.Fatal(err)
				}
				if err := r.strong.reconcile("absent"); err != nil {
					t.Fatal(err)
				}
			}

			got, ok := r.IsStrongReserved("pending")
			if !ok || !got.Equal(owner) {
				t.Fatalf("same-publication absence retired pending reservation: owner=%v reserved=%v", got, ok)
			}
		})
	}
}
