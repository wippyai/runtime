// SPDX-License-Identifier: MPL-2.0

//go:build linux

package affinity

import (
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
)

func TestApplyPinsCurrentThreadLinux(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var orig unix.CPUSet
	if err := unix.SchedGetaffinity(0, &orig); err != nil {
		t.Skipf("SchedGetaffinity unavailable: %v", err)
	}
	if orig.Count() < 2 {
		t.Skip("need at least 2 available CPUs")
	}

	target := -1
	for c := 0; c < 1024; c++ {
		if orig.IsSet(c) {
			target = c
			break
		}
	}
	if target < 0 {
		t.Skip("no usable CPU in affinity mask")
	}

	if err := Apply(Set{target}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var got unix.CPUSet
	if err := unix.SchedGetaffinity(0, &got); err != nil {
		t.Fatalf("SchedGetaffinity after Apply: %v", err)
	}
	if got.Count() != 1 || !got.IsSet(target) {
		t.Fatalf("expected affinity pinned to CPU %d only, got count=%d", target, got.Count())
	}

	if err := unix.SchedSetaffinity(0, &orig); err != nil {
		t.Fatalf("restore affinity: %v", err)
	}
}
