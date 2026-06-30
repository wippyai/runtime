// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestValidatePoolWorkerClass(t *testing.T) {
	cases := []struct {
		wantErr error
		name    string
		pool    PoolConfig
	}{
		{
			name: "worker class with workers only",
			pool: PoolConfig{WorkerClass: WorkerClassWASM, Workers: 4},
		},
		{
			name: "worker class with no sizing",
			pool: PoolConfig{WorkerClass: WorkerClassWASM},
		},
		{
			name: "arbitrary class name accepted",
			pool: PoolConfig{WorkerClass: "gpu"},
		},
		{
			name:    "negative values still rejected under class",
			pool:    PoolConfig{WorkerClass: WorkerClassWASM, Workers: -1},
			wantErr: ErrInvalidPoolConfig,
		},
		{
			name: "empty class keeps legacy flex semantics",
			pool: PoolConfig{},
		},
		{
			name:    "empty class worker pool still needs size",
			pool:    PoolConfig{Workers: 4},
			wantErr: ErrInvalidPoolSize,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePool(tc.pool)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("validatePool(%+v) = %v, want nil", tc.pool, err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("validatePool(%+v) = %v, want %v", tc.pool, err, tc.wantErr)
			}
		})
	}
}

// TestPoolConfigWorkerClassWireKey locks the JSON key to "worker_class" so the
// entry declaration stays source-compatible with the v2 runtime scheduler
// worker-class config.
func TestPoolConfigWorkerClassWireKey(t *testing.T) {
	raw, err := json.Marshal(PoolConfig{Type: PoolTypeStatic, WorkerClass: WorkerClassWASM})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"worker_class":"wasm"`) {
		t.Fatalf("PoolConfig JSON = %s, want worker_class key", raw)
	}

	var got PoolConfig
	if err := json.Unmarshal([]byte(`{"type":"static","worker_class":"wasm"}`), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.WorkerClass != WorkerClassWASM {
		t.Fatalf("WorkerClass = %q, want %q", got.WorkerClass, WorkerClassWASM)
	}
}

func TestFunctionConfigValidateAcceptsWorkerClass(t *testing.T) {
	cfg := FunctionConfig{
		FS:     "kickside.convert.doc2md:assets",
		Path:   "doc2md.wasm",
		Hash:   "sha256:deadbeef",
		Method: "convert",
		Pool:   PoolConfig{WorkerClass: WorkerClassWASM, Workers: 4},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
