package engine

import (
	"context"
	"testing"

	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Simple logging layer that counts yield events
type loggingLayer struct {
	yieldCount int
}

func (l *loggingLayer) Step(cvm CVM, tasks ...*Task) ([]*Task, error) {
	// Count yields
	for _, task := range tasks {
		if len(task.Yielded) > 0 {
			l.yieldCount++
		}
	}

	// only observe tasks, continue the execution chain
	return cvm.Step(tasks...)
}

// Simple validation layer that enforces a max value rule on yields
type execLayer struct {
	handled [][]lua.LValue
}

func (v *execLayer) Step(cvm CVM, tasks ...*Task) ([]*Task, error) {
	// terminate all tests and perform execution here
	for len(tasks) > 0 {
		// Process tasks that have yielded values
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				// Set empty resume values but don't modify the task otherwise
				task.Resumed = []lua.LValue{}
				v.handled = append(v.handled, task.Yielded)
			}
		}
		// Continue the execution chain with all tasks
		nextTasks, err := cvm.Step(tasks...)
		if err != nil {
			return nil, err
		}

		tasks = nextTasks
	}

	return nil, nil
}

func TestWrappedVM_Basic(t *testing.T) {
	logger := zap.NewNop()

	t.Run("logging layer counts yields", func(t *testing.T) {
		base, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer base.Close()

		loggingLayer := &loggingLayer{}
		exec := &execLayer{handled: make([][]lua.LValue, 0)}

		wvm := NewRunner(base, WithLayer(loggingLayer), WithLayer(exec))

		// Simple function that yields once and returns
		err = base.Import(`
            function test()
                coroutine.yield("first")
                coroutine.yield("second")
                return "done"
            end
        `, "test", "test")
		if err != nil {
			t.Fatal(err)
		}

		result, err := wvm.Execute(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}

		if result.String() != "done" {
			t.Errorf("unexpected result: got %v, want 'done'", result)
		}

		if loggingLayer.yieldCount != 2 {
			t.Errorf("unexpected yield count: got %d, want 2", loggingLayer.yieldCount)
		}
	})
}

func TestExecLayer_HandledValues(t *testing.T) {
	logger := zap.NewNop()

	t.Run("verify handled values", func(t *testing.T) {
		base, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer base.Close()

		exec := &execLayer{}
		wvm := NewRunner(base, WithLayer(exec))

		// Simpler test case first
		err = base.Import(`
            function test()
                coroutine.yield("first")
                coroutine.yield("second")
                return "done"
            end
        `, "test", "test")
		if err != nil {
			t.Fatal(err)
		}

		result, err := wvm.Execute(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}

		// Verify final result
		if result.String() != "done" {
			t.Errorf("unexpected result: got %v, want 'done'", result)
		}

		// Basic verification
		if len(exec.handled) == 0 {
			t.Fatal("no yields were handled")
		}

		// Verify number of yields
		expectedYields := 2
		if len(exec.handled) != expectedYields {
			t.Fatalf("expected %d yields, got %d", expectedYields, len(exec.handled))
		}

		// Verify first yield
		if len(exec.handled[0]) != 1 {
			t.Fatalf("first yield: expected 1 value, got %d", len(exec.handled[0]))
		}
		if exec.handled[0][0].String() != "first" {
			t.Errorf("first yield: expected 'first', got %q", exec.handled[0][0].String())
		}

		// Verify second yield
		if len(exec.handled[1]) != 1 {
			t.Fatalf("second yield: expected 1 value, got %d", len(exec.handled[1]))
		}
		if exec.handled[1][0].String() != "second" {
			t.Errorf("second yield: expected 'second', got %q", exec.handled[1][0].String())
		}
	})
}
