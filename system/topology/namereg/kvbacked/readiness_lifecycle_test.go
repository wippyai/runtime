// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

type readinessWatcher struct {
	ctx    context.Context
	events chan kvapi.WatchEvent
	closed chan struct{}
}

func (w *readinessWatcher) Events() <-chan kvapi.WatchEvent { return w.events }
func (w *readinessWatcher) Close() error                    { close(w.closed); return nil }

type readinessEngine struct {
	kvapi.Engine
	watcher *readinessWatcher
}

func (e *readinessEngine) Watch(ctx context.Context, _ string) (kvapi.Watcher, error) {
	e.watcher.ctx = ctx
	return e.watcher, nil
}

func TestReadinessClearedWhenReconcilerStops(t *testing.T) {
	for _, cancelContext := range []bool{false, true} {
		name := "watch closed"
		if cancelContext {
			name = "context canceled"
		}
		t.Run(name, func(t *testing.T) {
			r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
			watcher := &readinessWatcher{events: make(chan kvapi.WatchEvent), closed: make(chan struct{})}
			r.engine = &readinessEngine{Engine: r.engine, watcher: watcher}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := r.StartReconciler(ctx); err != nil {
				t.Fatal(err)
			}
			if !r.NameReady() {
				t.Fatal("reconciler did not become ready")
			}
			if cancelContext {
				cancel()
			} else {
				close(watcher.events)
			}
			select {
			case <-watcher.closed:
			case <-time.After(time.Second):
				t.Fatal("reconciler did not stop")
			}
			if r.NameReady() {
				t.Fatal("node remains ready after naming synchronization stopped")
			}
			if watcher.ctx.Err() == nil {
				t.Fatal("reconciliation worker context survived update-stream shutdown")
			}
		})
	}
}

type failedSeedEngine struct {
	*readinessEngine
	failure error
	prefix  string
}

func (e *failedSeedEngine) Scan(prefix string, fn func(kvapi.Entry) bool) error {
	if prefix == e.prefix {
		return e.failure
	}
	return e.Engine.Scan(prefix, fn)
}

func TestReadinessRequiresSuccessfulSeed(t *testing.T) {
	for _, prefix := range []string{pendingPrefix, activePrefix} {
		t.Run(prefix, func(t *testing.T) {
			r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
			watcher := &readinessWatcher{events: make(chan kvapi.WatchEvent), closed: make(chan struct{})}
			failure := errors.New("snapshot unavailable")
			r.engine = &failedSeedEngine{readinessEngine: &readinessEngine{Engine: r.engine, watcher: watcher}, failure: failure, prefix: prefix}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err := r.StartReconciler(ctx)
			if !errors.Is(err, failure) {
				t.Fatalf("failed seed reported successful startup: %v", err)
			}
			if r.NameReady() {
				t.Fatal("failed seed permitted name admission")
			}
			select {
			case <-watcher.closed:
			default:
				t.Fatal("failed startup leaked its watcher")
			}
		})
	}
}

func TestReadinessRejectsMalformedNamingSeed(t *testing.T) {
	for _, prefix := range []string{activePrefix, pendingPrefix} {
		for _, defect := range []string{"encoding", "name", "owner"} {
			t.Run(prefix+defect, func(t *testing.T) {
				r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
				key := prefix + "broken"
				var value []byte
				if defect == "encoding" {
					value = []byte{0xc1}
				} else {
					validPID := mkPID("node-1", "owner")
					name, owner := "broken", validPID.String()
					if defect == "name" {
						name = "different"
					} else {
						owner = "invalid"
					}
					var err error
					if prefix == activePrefix {
						value, err = encode(activeValue{Name: name, PID: owner, Strong: true})
					} else {
						value, err = encode(pendingHeader{Name: name, PID: owner, RequiredNodes: []pid.NodeID{"node-1"}})
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				if ok, err := r.engine.Txn([]kvapi.TxnOp{{Kind: kvapi.TxnPut, Key: key, Value: value}}); err != nil || !ok {
					t.Fatalf("seed: %v %v", ok, err)
				}
				watcher := &readinessWatcher{events: make(chan kvapi.WatchEvent), closed: make(chan struct{})}
				r.engine = &readinessEngine{Engine: r.engine, watcher: watcher}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				err := r.StartReconciler(ctx)
				if err == nil {
					t.Fatal("malformed naming seed was accepted")
				}
				if !strings.Contains(err.Error(), key) {
					t.Fatalf("error does not identify record: %v", err)
				}
				if r.NameReady() {
					t.Fatal("malformed seed opened name admission")
				}
				select {
				case <-watcher.closed:
				default:
					t.Fatal("failed startup retained its watcher")
				}
			})
		}
	}
}

func TestReadinessValidationPreservesAcceptedEmptyName(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	if _, err := r.Register(t.Context(), "", mkPID("node-1", "owner")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := r.StartReconciler(ctx); err != nil {
		t.Fatalf("seed validation rejected a name accepted by the registry: %v", err)
	}
	if !r.NameReady() {
		t.Fatal("valid stored record did not seed")
	}
}
