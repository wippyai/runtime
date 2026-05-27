// SPDX-License-Identifier: MPL-2.0

package pool

import (
	"sync"
	"testing"
	"time"
)

func TestAdmissionGateStopWaitsForAdmittedCalls(t *testing.T) {
	gate := NewAdmissionGate()
	if !gate.Begin() {
		t.Fatal("expected begin before stop")
	}

	stopped := make(chan struct{})
	go func() {
		gate.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
		t.Fatal("Stop returned before admitted call ended")
	case <-time.After(25 * time.Millisecond):
	}

	if gate.Begin() {
		t.Fatal("expected begin to fail after stop started")
	}

	gate.End()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return after admitted call ended")
	}
}

func TestAdmissionGateConcurrentBeginStop(t *testing.T) {
	gate := NewAdmissionGate()
	release := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !gate.Begin() {
				return
			}
			defer gate.End()
			<-release
		}()
	}

	stopped := make(chan struct{})
	go func() {
		gate.Stop()
		close(stopped)
	}()

	close(release)
	wg.Wait()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return")
	}

	if gate.Begin() {
		t.Fatal("expected gate to stay closed")
	}
}
