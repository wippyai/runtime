// SPDX-License-Identifier: MPL-2.0

package context

import (
	"context"
	"sync"
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

type sharedSet struct{ _ byte }

func (s *sharedSet) Clone() any { c := *s; return &c }

func TestContinueFrameContextSharesInheritedExecutionValues(t *testing.T) {
	key := &Key{Name: "test.shared", Execution: true, Inherit: true}
	ctx, fc := OpenFrameContext(context.Background())
	defer ReleaseFrameContext(fc)
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

type cloneRecorder struct{ clones *int }

func (c *cloneRecorder) Clone() any {
	*c.clones++
	return &cloneRecorder{clones: c.clones}
}

func TestContinueFrameContextDoesNotCloneExecutionValues(t *testing.T) {
	key := &Key{Name: "test.execution.clone", Execution: true, Inherit: true}
	ctx, frame := OpenFrameContext(context.Background())
	defer ReleaseFrameContext(frame)
	clones := 0
	if err := frame.Set(key, &cloneRecorder{clones: &clones}); err != nil {
		t.Fatal(err)
	}
	_, next, err := ContinueFrameContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseFrameContext(next)
	if clones != 0 {
		t.Fatalf("a continuation must not create and discard an inherited clone: got %d", clones)
	}
}

func TestContinueFrameContextClosesReplacementsItOwns(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(map[bool]string{false: "Set", true: "SetMultiple"}[batch], func(t *testing.T) {
			key := &Key{Name: "test.execution.replace", Execution: true}
			ctx, frame := OpenFrameContext(context.Background())
			defer ReleaseFrameContext(frame)
			original := &closeRecorder{}
			if err := frame.Set(key, original); err != nil {
				t.Fatal(err)
			}
			_, next, err := ContinueFrameContext(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer ReleaseFrameContext(next)
			replacement := &closeRecorder{}
			if batch {
				err = next.SetMultiple(Pair{Key: key, Value: replacement})
			} else {
				err = next.Set(key, replacement)
			}
			if err != nil {
				t.Fatal(err)
			}
			ReleaseFrameContext(next)
			if replacement.closed != 1 || original.closed != 0 {
				t.Fatalf("continuation owns the replacement only: replacement=%d original=%d", replacement.closed, original.closed)
			}
		})
	}
}

func TestContinueFrameContextResettingABorrowedHandleDoesNotOwnIt(t *testing.T) {
	key := &Key{Name: "test.execution.same", Execution: true}
	ctx, frame := OpenFrameContext(context.Background())
	defer ReleaseFrameContext(frame)
	original := &closeRecorder{}
	if err := frame.Set(key, original); err != nil {
		t.Fatal(err)
	}
	_, next, err := ContinueFrameContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseFrameContext(next)
	if err := next.Set(key, original); err != nil {
		t.Fatal(err)
	}
	if err := next.SetMultiple(Pair{Key: key, Value: original}); err != nil {
		t.Fatal(err)
	}
	if err := next.SetMultiple(Pair{Key: key, Value: &closeRecorder{}}, Pair{Key: key, Value: original}); err != nil {
		t.Fatal(err)
	}
	ReleaseFrameContext(next)
	if original.closed != 0 {
		t.Fatal("writing the same borrowed handle must not close its owner's resource")
	}
}

func TestContinueFrameContextConcurrentReplacementsAreOwned(t *testing.T) {
	ctx, frame := OpenFrameContext(context.Background())
	defer ReleaseFrameContext(frame)
	keys := make([]*Key, 16)
	originals := make([]*closeRecorder, len(keys))
	replacements := make([]*closeRecorder, len(keys))
	for i := range keys {
		keys[i] = &Key{Execution: true}
		originals[i], replacements[i] = &closeRecorder{}, &closeRecorder{}
		if err := frame.Set(keys[i], originals[i]); err != nil {
			t.Fatal(err)
		}
	}
	_, next, err := ContinueFrameContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseFrameContext(next)
	var writes sync.WaitGroup
	for i := range keys {
		writes.Go(func() {
			if err := next.Set(keys[i], replacements[i]); err != nil {
				t.Error(err)
			}
		})
	}
	writes.Wait()
	ReleaseFrameContext(next)
	for i := range keys {
		if originals[i].closed != 0 || replacements[i].closed != 1 {
			t.Fatalf("key %d: original=%d replacement=%d", i, originals[i].closed, replacements[i].closed)
		}
	}
}

func TestContinueFrameContextRestoresBorrowedOwnership(t *testing.T) {
	key := &Key{Name: "test.execution.restore", Execution: true}
	ctx, frame := OpenFrameContext(context.Background())
	defer ReleaseFrameContext(frame)
	original, replacement := &closeRecorder{}, &closeRecorder{}
	if err := frame.Set(key, original); err != nil {
		t.Fatal(err)
	}
	_, next, err := ContinueFrameContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseFrameContext(next)
	if err := next.Set(key, replacement); err != nil {
		t.Fatal(err)
	}
	if err := next.Set(key, original); err != nil {
		t.Fatal(err)
	}
	// As for ordinary frame writes, the caller owns an overwritten value.
	_ = replacement.Close()
	ReleaseFrameContext(next)
	if original.closed != 0 {
		t.Fatal("restoring the root's handle must restore borrowing, not transfer ownership")
	}
}

func TestContinueFrameContextConcurrentWritesKeepValueAndOwnershipTogether(t *testing.T) {
	key := &Key{Name: "test.execution.concurrent", Execution: true}
	ctx, frame := OpenFrameContext(context.Background())
	defer ReleaseFrameContext(frame)
	original, replacement := &closeRecorder{}, &closeRecorder{}
	if err := frame.Set(key, original); err != nil {
		t.Fatal(err)
	}
	_, next, err := ContinueFrameContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseFrameContext(next)
	var writes sync.WaitGroup
	for i := range 32 {
		writes.Go(func() {
			value := original
			if i%2 == 0 {
				value = replacement
			}
			if err := next.SetMultiple(Pair{Key: key, Value: value}); err != nil {
				t.Error(err)
			}
		})
	}
	writes.Wait()
	value, _ := next.Get(key)
	ReleaseFrameContext(next)
	if original.closed != 0 {
		t.Fatal("concurrent writes transferred ownership of the root's handle")
	}
	if value == replacement && replacement.closed != 1 {
		t.Fatal("the final replacement must remain owned by the continuation")
	}
	if value == original && replacement.closed != 0 {
		t.Fatal("an overwritten replacement must not be closed by the frame")
	}
}
