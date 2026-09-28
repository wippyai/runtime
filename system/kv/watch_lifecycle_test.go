// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"

	hraft "github.com/hashicorp/raft"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// An invalidated observer must be visible without requiring a consumer to read
// another event. Events eventually closes even when that consumer stopped.
func requireInvalidatedWatch(t *testing.T, w kvapi.Watcher, reason error) {
	t.Helper()
	select {
	case <-w.Done():
	default:
		t.Error("watch remained valid after source invalidation")
	}
	if !errors.Is(w.Err(), reason) {
		t.Errorf("invalidation reason: got %v, want %v", w.Err(), reason)
	}
	synctest.Wait()
	select {
	case _, open := <-w.Events():
		if open {
			t.Error("invalidated watch retained an event")
		}
	default:
		t.Error("invalidated watch delivery worker remained alive")
	}
}

func TestWatchLifecycleRestoreInvalidatesPrefixWatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fsm := NewRaftFSM()
		engine := NewRaftEngine(&fakeRaft{fsm: fsm, leader: true}, fsm, "node", nil, nil)
		watch, err := engine.Watch(t.Context(), "matching/")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = watch.Close() })
		fsm.Apply(&hraft.Log{Index: 1, Data: encodeCommand(command{Op: opSet, Key: "matching/key", Value: []byte("old")})})
		<-watch.Events()
		if err := fsm.Restore(io.NopCloser(bytes.NewReader(nil))); err != nil {
			t.Fatal(err)
		}
		requireInvalidatedWatch(t, watch, kvapi.ErrWatchReset)
	})
}

func TestWatchLifecycleEngineStopInvalidatesWatch(t *testing.T) {
	for _, backend := range []string{"standalone", "raft", "crdt"} {
		t.Run(backend, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var engine kvapi.Engine
				var stop func() error
				switch backend {
				case "standalone":
					svc := NewService("watch-stop", nil)
					if _, err := svc.Start(t.Context()); err != nil {
						t.Fatal(err)
					}
					engine = svc
					stop = func() error { return svc.Stop(context.Background()) }
				case "raft":
					fsm := NewRaftFSM()
					svc := NewRaftEngine(&fakeRaft{fsm: fsm, leader: true}, fsm, "node", nil, nil)
					if err := svc.Start(t.Context()); err != nil {
						t.Fatal(err)
					}
					engine, stop = svc, svc.Stop
				case "crdt":
					svc := NewCRDTEngine("node", nil)
					if err := svc.Start(t.Context()); err != nil {
						t.Fatal(err)
					}
					engine, stop = svc, svc.Stop
				}
				// Deliberately keep the caller alive after the source stops.
				watch, err := engine.Watch(context.Background(), "")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = watch.Close() })
				if err := stop(); err != nil {
					t.Fatal(err)
				}
				requireInvalidatedWatch(t, watch, kvapi.ErrKVClosed)
			})
		})
	}
}

func TestWatchLifecycleRaftParentCancellationInvalidatesBackgroundWatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fsm := NewRaftFSM()
		engine := NewRaftEngine(&fakeRaft{fsm: fsm, leader: true}, fsm, "node", nil, nil)
		ctx, cancel := context.WithCancel(t.Context())
		if err := engine.Start(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = engine.Stop() })
		watch, err := engine.Watch(context.Background(), "prefix/")
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		synctest.Wait()
		requireInvalidatedWatch(t, watch, kvapi.ErrKVClosed)
		if _, err := engine.Watch(context.Background(), ""); !errors.Is(err, kvapi.ErrKVClosed) {
			t.Fatalf("canceled engine admitted another watcher: %v", err)
		}
	})
}

func TestWatchLifecycleRejectedRestoreLeavesOldGenerationUsable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fsm := NewRaftFSM()
		engine := NewRaftEngine(&fakeRaft{fsm: fsm, leader: true}, fsm, "node", nil, nil)
		watch, err := engine.Watch(t.Context(), "k")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = watch.Close() })
		if err := fsm.Restore(io.NopCloser(bytes.NewReader([]byte{0xff, 0x0, 0xff}))); err == nil {
			t.Fatal("malformed snapshot was accepted")
		}
		select {
		case <-watch.Done():
			t.Fatalf("failed restore invalidated a healthy watch: %v", watch.Err())
		default:
		}
		fsm.Apply(&hraft.Log{Index: 1, Data: encodeCommand(command{Op: opSet, Key: "k", Value: []byte("live")})})
		ev := <-watch.Events()
		if ev.Current == nil || string(ev.Current.Value) != "live" {
			t.Fatalf("healthy watch missed later publication: %+v", ev)
		}
	})
}

func TestWatchLifecycleStoppingOneRaftFacadePreservesAnother(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fsm := NewRaftFSM()
		leader := &fakeRaft{fsm: fsm, leader: true}
		first := NewRaftEngine(leader, fsm, "first", nil, nil)
		second := NewRaftEngine(leader, fsm, "second", nil, nil)
		a, err := first.Watch(t.Context(), "key")
		if err != nil {
			t.Fatal(err)
		}
		b, err := second.Watch(t.Context(), "key")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = b.Close() })
		if err := first.Stop(); err != nil {
			t.Fatal(err)
		}
		requireInvalidatedWatch(t, a, kvapi.ErrKVClosed)
		select {
		case <-b.Done():
			t.Fatalf("unrelated facade's watch invalidated: %v", b.Err())
		default:
		}
		fsm.Apply(&hraft.Log{Index: 1, Data: encodeCommand(command{Op: opSet, Key: "key", Value: []byte("v")})})
		if ev := <-b.Events(); ev.Current == nil || string(ev.Current.Value) != "v" {
			t.Fatalf("surviving facade missed publication: %+v", ev)
		}
	})
}

func TestWatchLifecycleCRDTDeleteRetainsPrefixKey(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		engine := NewCRDTEngine("node", nil)
		if err := engine.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = engine.Stop() })
		watch, err := engine.Watch(t.Context(), "key/")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = watch.Close() })
		if _, err := engine.Set("key/k", []byte("v")); err != nil {
			t.Fatal(err)
		}
		<-watch.Events()
		if err := engine.Delete("key/k"); err != nil {
			t.Fatal(err)
		}
		ev := <-watch.Events()
		if ev.Type != kvapi.WatchDelete || ev.Current != nil || ev.Previous == nil || ev.Previous.Key != "key/k" {
			t.Fatalf("prefix watcher missed CRDT deletion key: %+v", ev)
		}
	})
}
