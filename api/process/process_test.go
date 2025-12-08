package process

import (
	"testing"
)

func TestStepOutput_Result(t *testing.T) {
	tests := []struct {
		name   string
		result Payload
		want   any
	}{
		{
			name:   "nil result",
			result: nil,
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var so StepOutput
			if tt.result != nil {
				so.Done(tt.result)
			} else {
				so.Done(nil)
			}

			if tt.result == nil {
				if so.Result() != nil {
					t.Errorf("expected nil result, got %v", so.Result())
				}
				return
			}
		})
	}
}

func TestStepOutput_Reset(t *testing.T) {
	var so StepOutput
	so.Done(nil)
	so.Yield(&mockCommand{id: 1}, 0)

	so.Reset()

	if so.Status() != StepContinue {
		t.Errorf("Status after Reset = %v, want StepContinue", so.Status())
	}
	if so.Result() != nil {
		t.Errorf("Result after Reset = %v, want nil", so.Result())
	}
	if so.Count() != 0 {
		t.Errorf("Count after Reset = %d, want 0", so.Count())
	}
}

func TestStepOutput_Yield(t *testing.T) {
	var so StepOutput

	for i := 0; i < MaxYields+2; i++ {
		so.Yield(&mockCommand{id: CommandID(i)}, uint64(i+1)) //nolint:gosec // test iteration
	}

	if so.Count() != MaxYields+2 {
		t.Errorf("Count = %d, want %d", so.Count(), MaxYields+2)
	}

	yields := so.Yields()
	if len(yields) != MaxYields+2 {
		t.Errorf("len(Yields) = %d, want %d", len(yields), MaxYields+2)
	}
}

type mockCommand struct {
	id CommandID
}

func (c *mockCommand) CmdID() CommandID { return c.id }
