// SPDX-License-Identifier: MPL-2.0

package context

import (
	"context"
	"testing"
)

type closeRecorder struct{ closed int }

func (c *closeRecorder) Close() error {
	c.closed++
	return nil
}

func TestContinueFrameContext(t *testing.T) {
	execKey := &Key{Name: "test.execution", Execution: true}
	inheritKey := &Key{Name: "test.inherit", Inherit: true}
	localKey := &Key{Name: "test.local"}

	ctx, fc := OpenFrameContext(context.Background())
	if err := fc.SetMultiple(
		Pair{Key: execKey, Value: "exec"},
		Pair{Key: inheritKey, Value: "inherit"},
		Pair{Key: localKey, Value: "local"},
	); err != nil {
		t.Fatal(err)
	}
	fc.Seal()

	next, nfc, err := ContinueFrameContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := nfc.Get(execKey); v != "exec" {
		t.Errorf("execution value carries to the continuing frame, got %v", v)
	}
	if v, _ := nfc.Get(inheritKey); v != "inherit" {
		t.Errorf("inheritable value carries to the continuing frame, got %v", v)
	}
	if nfc.Has(localKey) {
		t.Error("frame-local value stays with its frame")
	}

	nfc.Seal()
	_, ffc := OpenFrameContext(next)
	defer ReleaseFrameContext(ffc)
	if ffc.Has(execKey) {
		t.Error("execution value does not pass to a forked frame")
	}
	ReleaseFrameContext(nfc)
	ReleaseFrameContext(fc)
}

func TestContinueFrameContextLeavesExecutionValuesToTheirFrame(t *testing.T) {
	execKey := &Key{Name: "test.execution.closer", Execution: true}
	ctx, fc := OpenFrameContext(context.Background())
	port := &closeRecorder{}
	if err := fc.Set(execKey, port); err != nil {
		t.Fatal(err)
	}

	_, nfc, err := ContinueFrameContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !fc.IsSealed() {
		t.Error("the execution frame is sealed once its code continues elsewhere")
	}
	if v, _ := nfc.Get(execKey); v != port {
		t.Fatal("the continuation refers to the execution's value")
	}
	ReleaseFrameContext(nfc)
	if port.closed != 0 {
		t.Fatal("releasing a continuation leaves the execution's value open")
	}
	ReleaseFrameContext(fc)
	if port.closed != 1 {
		t.Fatalf("the execution's frame closes its value once, got %d", port.closed)
	}
}

func TestContinueFrameContextWithoutFrame(t *testing.T) {
	ctx, fc, err := ContinueFrameContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseFrameContext(fc)
	if FrameFromContext(ctx) == nil {
		t.Fatal("an execution without a frame continues in a new frame")
	}
}

func TestContinueFrameContextClosesValuesItOwns(t *testing.T) {
	borrowedKey := &Key{Name: "test.borrowed", Execution: true}
	ownKey := &Key{Name: "test.own", Execution: true}
	ctx, fc := OpenFrameContext(context.Background())
	borrowed := &closeRecorder{}
	if err := fc.Set(borrowedKey, borrowed); err != nil {
		t.Fatal(err)
	}

	_, nfc, err := ContinueFrameContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	own := &closeRecorder{}
	if err := nfc.Set(ownKey, own); err != nil {
		t.Fatal(err)
	}
	ReleaseFrameContext(nfc)
	if own.closed != 1 {
		t.Fatalf("a value the continuation installed is closed with it, got %d", own.closed)
	}
	if borrowed.closed != 0 {
		t.Fatal("a value borrowed from the execution frame stays open")
	}
	ReleaseFrameContext(fc)
	if borrowed.closed != 1 {
		t.Fatalf("the execution frame closes its value once, got %d", borrowed.closed)
	}
}

type sharedSet struct{ n int }

func (s *sharedSet) Clone() any { c := *s; return &c }

func TestContinueFrameContextSharesInheritedExecutionValues(t *testing.T) {
	key := &Key{Name: "test.shared", Execution: true, Inherit: true}
	ctx, fc := OpenFrameContext(context.Background())
	set := &sharedSet{}
	if err := fc.Set(key, set); err != nil {
		t.Fatal(err)
	}

	_, nfc, err := ContinueFrameContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseFrameContext(nfc)
	if v, _ := nfc.Get(key); v != set {
		t.Fatal("a continuation shares an inheritable execution value instead of copying it")
	}
}
