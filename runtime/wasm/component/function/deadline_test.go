// SPDX-License-Identifier: MPL-2.0
package function

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	processapi "github.com/wippyai/runtime/api/process"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	wasmengine "github.com/wippyai/runtime/runtime/wasm/engine"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

// Bound the regression externally in case interruption stops working.
func TestFunctionRuntimeDeadlineInterruptsGuestLoop(t *testing.T) {
	if os.Getenv("W1_FUNCTION_DEADLINE_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFunctionRuntimeDeadlineInterruptsGuestLoop$")
		cmd.Env = append(os.Environ(), "W1_FUNCTION_DEADLINE_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("function deadline failed: %v\n%s", err, output)
		}
		return
	}
	ctx := context.Background()
	m := NewManager(zap.NewNop(), nil, noopDispatcher{}, nil)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	for _, rt := range []*wasmrt.Runtime{m.coreRT, m.componentRT} {
		mod, err := rt.LoadWAT(ctx, `(module (func (export "run") (loop $forever br $forever)))`, `run: func();`)
		if err != nil {
			t.Fatal(err)
		}
		p := wasmengine.NewProcess(mod, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{MaxExecutionMS: 20}, nil)
		if err = p.Init(ctx, "run", nil); err != nil {
			t.Fatal(err)
		}
		var out processapi.StepOutput
		err = p.Step(nil, &out)
		p.Close()
		if err == nil || !strings.Contains(err.Error(), "deadline") {
			t.Fatalf("guest loop result: %v", err)
		}
	}
}
