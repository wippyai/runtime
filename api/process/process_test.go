package process

import (
	"testing"

	"github.com/wippyai/runtime/api/payload"
)

func TestStepResult_Result(t *testing.T) {
	tests := []struct {
		name   string
		result payload.Payload
		want   any
	}{
		{
			name:   "nil result",
			result: nil,
			want:   nil,
		},
		{
			name:   "string result",
			result: payload.New("hello"),
			want:   "hello",
		},
		{
			name:   "int result",
			result: payload.New(42),
			want:   42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := StepResult{
				Status: StepDone,
				Result: tt.result,
			}

			if tt.result == nil {
				if sr.Result != nil {
					t.Errorf("expected nil result, got %v", sr.Result)
				}
				return
			}

			if sr.Result == nil {
				t.Errorf("expected result, got nil")
				return
			}

			if sr.Result.Data() != tt.want {
				t.Errorf("Result.Data() = %v, want %v", sr.Result.Data(), tt.want)
			}
		})
	}
}

func TestStepResult_Reset(t *testing.T) {
	sr := StepResult{
		Status: StepDone,
		Result: payload.New("test"),
	}
	sr.AddYield(&mockCommand{id: 1})

	sr.Reset()

	if sr.Status != StepContinue {
		t.Errorf("Status after Reset = %v, want StepContinue", sr.Status)
	}
	if sr.Result != nil {
		t.Errorf("Result after Reset = %v, want nil", sr.Result)
	}
	if sr.YieldCount() != 0 {
		t.Errorf("YieldCount after Reset = %d, want 0", sr.YieldCount())
	}
}

func TestStepResult_AddYield(t *testing.T) {
	sr := StepResult{}

	for i := 0; i < MaxYields+2; i++ {
		sr.AddYield(&mockCommand{id: CommandID(i)})
	}

	if sr.YieldCount() != MaxYields+2 {
		t.Errorf("YieldCount = %d, want %d", sr.YieldCount(), MaxYields+2)
	}

	yields := sr.GetYields()
	if len(yields) != MaxYields+2 {
		t.Errorf("len(GetYields) = %d, want %d", len(yields), MaxYields+2)
	}
}

type mockCommand struct {
	id CommandID
}

func (c *mockCommand) CmdID() CommandID { return c.id }
