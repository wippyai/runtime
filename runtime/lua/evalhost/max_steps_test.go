package evalhost

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

// A pure-Lua busy loop consumes one scheduler step because it does not yield.
// These cases pin the explicit-limit and omitted-limit semantics at the host
// boundary; yield-heavy enforcement is covered by the runner integration test.
func TestHost_Run_MaxSteps(t *testing.T) {
	host := NewHost(zap.NewNop(), safeModulesProvider())

	longLoop := `
		return {
			run = function()
				local n = 0
				for i = 1, 1000000 do
					n = n + 1
				end
				return n
			end
		}
	`

	t.Run("omitted limit uses the host default", func(t *testing.T) {
		result, err := host.Run(context.Background(), RunCmd{
			Source: longLoop,
			Method: "run",
		})
		if err != nil {
			t.Fatalf("expected completion without a step limit, got %v", err)
		}
		if result == nil {
			t.Fatal("expected a result")
		}
	})

	t.Run("explicit limit admits work within the budget", func(t *testing.T) {
		result, err := host.Run(context.Background(), RunCmd{
			Source:      longLoop,
			Method:      "run",
			MaxSteps:    1,
			MaxStepsSet: true,
		})
		if err != nil {
			t.Fatalf("expected completion within the explicit budget, got %v", err)
		}
		if result == nil {
			t.Fatal("expected a result")
		}
	})
}

func TestHost_DefaultMaxSteps(t *testing.T) {
	host := NewHost(zap.NewNop(), safeModulesProvider())
	if host.defaultSteps != DefaultMaxSteps {
		t.Fatalf("default steps = %d, want %d", host.defaultSteps, DefaultMaxSteps)
	}

	unlimited := NewHost(zap.NewNop(), safeModulesProvider(), WithDefaultMaxSteps(0))
	if unlimited.defaultSteps != 0 {
		t.Fatalf("configured default steps = %d, want unlimited", unlimited.defaultSteps)
	}
}
