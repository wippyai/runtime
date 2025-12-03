package build

import (
	"context"
	"errors"
	"testing"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/registry"
)

func TestPipeline_Execute_EmptyPipeline(t *testing.T) {
	pipeline := New()
	entries := make([]registry.Entry, 0)

	err := pipeline.Execute(context.Background(), &entries)
	if err != nil {
		t.Errorf("empty pipeline should not error, got: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestPipeline_Execute_SingleStage(t *testing.T) {
	called := false
	stageFn := func(_ context.Context, entries *[]registry.Entry) error {
		called = true
		*entries = append(*entries, registry.Entry{
			ID: registry.NewID("test", "entry1"),
		})
		return nil
	}

	pipeline := New(newTestStage("add_entry", stageFn))
	entries := make([]registry.Entry, 0)

	err := pipeline.Execute(context.Background(), &entries)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !called {
		t.Error("stage was not called")
	}

	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

// testStage is a simple stage implementation for testing
type testStage struct {
	name string
	fn   func(context.Context, *[]registry.Entry) error
}

func (s *testStage) Name() string { return s.name }

func (s *testStage) Execute(ctx context.Context, entries *[]registry.Entry) error {
	return s.fn(ctx, entries)
}

func newTestStage(name string, fn func(context.Context, *[]registry.Entry) error) boot.Stage {
	return &testStage{name: name, fn: fn}
}

func TestPipeline_Execute_MultipleStages(t *testing.T) {
	stage1 := func(_ context.Context, entries *[]registry.Entry) error {
		*entries = append(*entries, registry.Entry{
			ID: registry.NewID("test", "entry1"),
		})
		return nil
	}

	stage2 := func(_ context.Context, entries *[]registry.Entry) error {
		*entries = append(*entries, registry.Entry{
			ID: registry.NewID("test", "entry2"),
		})
		return nil
	}

	stage3 := func(_ context.Context, entries *[]registry.Entry) error {
		*entries = append(*entries, registry.Entry{
			ID: registry.NewID("test", "entry3"),
		})
		return nil
	}

	pipeline := New(
		newTestStage("stage1", stage1),
		newTestStage("stage2", stage2),
		newTestStage("stage3", stage3),
	)

	entries := make([]registry.Entry, 0)
	err := pipeline.Execute(context.Background(), &entries)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}

	if entries[0].ID.Name != "entry1" || entries[1].ID.Name != "entry2" || entries[2].ID.Name != "entry3" {
		t.Error("entries not in expected order")
	}
}

func TestPipeline_Execute_StageModifiesEntries(t *testing.T) {
	addStage := func(_ context.Context, entries *[]registry.Entry) error {
		*entries = append(*entries, registry.Entry{
			ID: registry.NewID("test", "entry1"),
		})
		return nil
	}

	modifyStage := func(_ context.Context, entries *[]registry.Entry) error {
		for i := range *entries {
			(*entries)[i].Meta = map[string]interface{}{"modified": true}
		}
		return nil
	}

	pipeline := New(
		newTestStage("add", addStage),
		newTestStage("modify", modifyStage),
	)

	entries := make([]registry.Entry, 0)
	err := pipeline.Execute(context.Background(), &entries)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].Meta == nil {
		t.Error("entry metadata not set")
	}

	if modified, ok := entries[0].Meta["modified"].(bool); !ok || !modified {
		t.Error("entry not modified correctly")
	}
}

func TestPipeline_Execute_ErrorPropagation(t *testing.T) {
	expectedErr := errors.New("stage error")
	errorStage := func(_ context.Context, _ *[]registry.Entry) error {
		return expectedErr
	}

	pipeline := New(newTestStage("failing_stage", errorStage))
	entries := make([]registry.Entry, 0)

	err := pipeline.Execute(context.Background(), &entries)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error to wrap original error, got: %v", err)
	}

	expectedMsg := "stage 'failing_stage'"
	if err.Error()[:len(expectedMsg)] != expectedMsg {
		t.Errorf("error message should include stage name, got: %v", err)
	}
}

func TestPipeline_Execute_StopsOnFirstError(t *testing.T) {
	stage1Called := false
	stage2Called := false
	stage3Called := false

	stage1 := func(_ context.Context, _ *[]registry.Entry) error {
		stage1Called = true
		return nil
	}

	stage2 := func(_ context.Context, _ *[]registry.Entry) error {
		stage2Called = true
		return errors.New("stage2 error")
	}

	stage3 := func(_ context.Context, _ *[]registry.Entry) error {
		stage3Called = true
		return nil
	}

	pipeline := New(
		newTestStage("stage1", stage1),
		newTestStage("stage2", stage2),
		newTestStage("stage3", stage3),
	)

	entries := make([]registry.Entry, 0)
	err := pipeline.Execute(context.Background(), &entries)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !stage1Called {
		t.Error("stage1 should have been called")
	}

	if !stage2Called {
		t.Error("stage2 should have been called")
	}

	if stage3Called {
		t.Error("stage3 should not have been called after stage2 error")
	}
}

func TestPipeline_Execute_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stage := func(ctx context.Context, _ *[]registry.Entry) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}

	pipeline := New(newTestStage("check_cancel", stage))
	entries := make([]registry.Entry, 0)

	err := pipeline.Execute(ctx, &entries)

	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
}

func TestPipeline_Execute_PreservesPointerReference(t *testing.T) {
	stage := func(_ context.Context, entries *[]registry.Entry) error {
		*entries = append(*entries, registry.Entry{
			ID: registry.NewID("test", "entry1"),
		})
		return nil
	}

	pipeline := New(newTestStage("add", stage))
	entries := make([]registry.Entry, 0)
	originalPtr := &entries

	err := pipeline.Execute(context.Background(), &entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if originalPtr != &entries {
		t.Error("pointer reference changed")
	}

	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

func TestPipeline_Execute_StageOrder(t *testing.T) {
	order := make([]string, 0)

	stage1 := func(_ context.Context, _ *[]registry.Entry) error {
		order = append(order, "first")
		return nil
	}

	stage2 := func(_ context.Context, _ *[]registry.Entry) error {
		order = append(order, "second")
		return nil
	}

	stage3 := func(_ context.Context, _ *[]registry.Entry) error {
		order = append(order, "third")
		return nil
	}

	pipeline := New(
		newTestStage("first", stage1),
		newTestStage("second", stage2),
		newTestStage("third", stage3),
	)

	entries := make([]registry.Entry, 0)
	err := pipeline.Execute(context.Background(), &entries)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(order) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(order))
	}

	if order[0] != "first" || order[1] != "second" || order[2] != "third" {
		t.Errorf("stages executed in wrong order: %v", order)
	}
}

func TestNewTestStage_CreatesNamedStage(t *testing.T) {
	stageFn := func(_ context.Context, _ *[]registry.Entry) error {
		return nil
	}

	s := newTestStage("test_stage", stageFn)

	if s.Name() != "test_stage" {
		t.Errorf("expected name 'test_stage', got '%s'", s.Name())
	}

	err := s.Execute(context.Background(), &[]registry.Entry{})
	if err != nil {
		t.Error("stage function should execute without error")
	}
}

func TestPipeline_InterfaceCompliance(_ *testing.T) {
	var _ boot.Pipeline = &pipeline{}
}
