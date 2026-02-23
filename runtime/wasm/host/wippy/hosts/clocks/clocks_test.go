// SPDX-License-Identifier: MPL-2.0

package clocks

import (
	"context"
	"testing"
	"time"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestWallClockHost(t *testing.T) {
	host := NewWallClockHost()
	if host.Namespace() != WallClockNamespace {
		t.Fatalf("Namespace() = %q, want %q", host.Namespace(), WallClockNamespace)
	}

	now := host.Now(context.Background())
	if now.Seconds == 0 {
		t.Fatalf("Now().Seconds = 0, expected unix timestamp")
	}
	if now.Nanoseconds > 999999999 {
		t.Fatalf("Now().Nanoseconds = %d, want <= 999999999", now.Nanoseconds)
	}

	res := host.Resolution(context.Background())
	if res.Seconds != 1 || res.Nanoseconds != 0 {
		t.Fatalf("Resolution() = %#v, want {Seconds:1 Nanoseconds:0}", res)
	}
}

func TestMonotonicClockHost(t *testing.T) {
	host := NewMonotonicClockHost(preview2.NewResourceTable())
	if host.Namespace() != MonotonicClockNamespace {
		t.Fatalf("Namespace() = %q, want %q", host.Namespace(), MonotonicClockNamespace)
	}

	first := host.Now(context.Background())
	time.Sleep(1 * time.Millisecond)
	second := host.Now(context.Background())
	if second < first {
		t.Fatalf("Now() regressed: first=%d second=%d", first, second)
	}

	if got := host.Resolution(context.Background()); got != 1 {
		t.Fatalf("Resolution() = %d, want 1", got)
	}
}

func TestMonotonicClockHost_SubscribeDuration(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewMonotonicClockHost(resources)

	handle := host.SubscribeDuration(context.Background(), uint64(5*time.Millisecond))
	if handle == 0 {
		t.Fatal("SubscribeDuration() returned zero handle")
	}

	r, ok := resources.Get(handle)
	if !ok {
		t.Fatal("pollable handle not found")
	}
	p, ok := r.(preview2.Pollable)
	if !ok {
		t.Fatalf("resource type = %T, want preview2.Pollable", r)
	}
	if p.Ready() {
		t.Fatal("pollable should not be ready immediately")
	}

	time.Sleep(8 * time.Millisecond)
	if !p.Ready() {
		t.Fatal("pollable should be ready after duration")
	}
}
