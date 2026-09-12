// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNameGuardCancellationAndIndependentNames(t *testing.T) {
	var g NameGuard
	release, err := g.LockContext(context.Background(), "held")
	if err != nil {
		t.Fatal(err)
	}
	other, err := g.LockContext(context.Background(), "other")
	if err != nil {
		t.Fatal(err)
	}
	other()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if acquired, err := g.LockContext(ctx, "held"); err == nil || acquired != nil {
		t.Fatal("canceled caller acquired admission")
	}
	release()
	if len(g.names) != 0 {
		t.Fatal("idle names retained")
	}
}

func TestNameGuardSerializesContendedName(t *testing.T) {
	var g NameGuard
	var wg sync.WaitGroup
	count := 0
	for range 16 {
		wg.Go(func() {
			for range 100 {
				release, err := g.LockContext(context.Background(), "claim")
				if err != nil {
					t.Error(err)
					return
				}
				count++ // deliberately not atomic: race detector verifies mutual exclusion
				release()
			}
		})
	}
	wg.Wait()
	if count != 1600 {
		t.Fatalf("count=%d", count)
	}
	if len(g.names) != 0 {
		t.Fatal("idle names retained")
	}
}

func BenchmarkNameGuard(b *testing.B) {
	for _, shared := range []bool{false, true} {
		label := "independent"
		if shared {
			label = "contended"
		}
		b.Run(label, func(b *testing.B) {
			var guard NameGuard
			var worker atomic.Uint64
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				name := strconv.FormatUint(worker.Add(1), 10)
				if shared {
					name = "shared"
				}
				for pb.Next() {
					release, err := guard.LockContext(context.Background(), name)
					if err != nil {
						b.Fatal(err)
					}
					release()
				}
			})
		})
	}
}

func TestNameGuardCloseJoinsHoldersAndRefusesWaiters(t *testing.T) {
	var guard NameGuard
	first, err := guard.LockContext(context.Background(), "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := guard.LockContext(context.Background(), "second")
	if err != nil {
		t.Fatal(err)
	}
	queued := make(chan error, 1)
	go func() {
		release, err := guard.LockContext(context.Background(), "first")
		if release != nil {
			release()
		}
		queued <- err
	}()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	closeErr := guard.Close(canceled)
	first()
	second()
	if !errors.Is(closeErr, context.Canceled) {
		t.Fatalf("close returned before holders released: %v", closeErr)
	}
	select {
	case err := <-queued:
		if !errors.Is(err, ErrNameAdmissionClosed) {
			t.Fatalf("queued admission: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not wake queued admission")
	}
	joined, done := context.WithTimeout(context.Background(), time.Second)
	defer done()
	if err := guard.Close(joined); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first", "new"} {
		release, err := guard.LockContext(context.Background(), name)
		if release != nil {
			release()
			t.Fatal("closed guard returned ownership")
		}
		if !errors.Is(err, ErrNameAdmissionClosed) {
			t.Fatalf("new admission: %v", err)
		}
	}
}
func TestNameGuardCloseEmptyIsTerminal(t *testing.T) {
	var guard NameGuard
	if err := guard.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := guard.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := guard.LockContext(context.Background(), "name"); !errors.Is(err, ErrNameAdmissionClosed) {
		t.Fatal(err)
	}
}
