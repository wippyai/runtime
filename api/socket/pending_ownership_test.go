// SPDX-License-Identifier: MPL-2.0
package socket

import (
	"context"
	"errors"
	"testing"
	"time"
)

type ownershipBlockedCloser struct {
	entered chan struct{}
	release chan struct{}
}

func (c *ownershipBlockedCloser) Close() error { close(c.entered); <-c.release; return nil }

func TestPendingRejectedResultClosesBeforeCompletion(t *testing.T) {
	op := NewPendingOperation()
	_, started := op.Start(context.Background())
	if !started {
		t.Fatal("operation not started")
	}
	closer := &ownershipBlockedCloser{entered: make(chan struct{}), release: make(chan struct{})}
	workerDone := make(chan struct{})
	go func() { op.Complete(closer, errors.New("rejected result")); close(workerDone) }()
	t.Cleanup(func() {
		close(closer.release)
		op.Close()
		select {
		case <-workerDone:
		case <-time.After(time.Second):
			t.Error("completion worker did not exit")
		}
	})
	select {
	case <-closer.entered:
	case <-time.After(time.Second):
		t.Fatal("rejected result not closed")
	}
	if op.Ready() {
		t.Fatal("published completion before physical result close finished")
	}
	value, err, ready := op.Take()
	if ready || value != nil || err != nil {
		t.Fatalf("took result while physical close pending: %v %v %v", value, err, ready)
	}
}

func TestPendingNilSuccessIsError(t *testing.T) {
	op := NewPendingOperation()
	_, started := op.Start(context.Background())
	if !started {
		t.Fatal("operation not started")
	}
	defer op.Close()
	op.Complete(nil, nil)
	value, err, ready := op.Take()
	if !ready || err == nil || value != nil {
		t.Fatalf("invalid nil success accepted: %v %v %v", value, err, ready)
	}
}
