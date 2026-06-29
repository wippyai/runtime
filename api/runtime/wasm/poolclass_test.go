// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"errors"
	"testing"
)

func TestValidatePoolClass(t *testing.T) {
	cases := []struct {
		wantErr error
		name    string
		pool    PoolConfig
	}{
		{
			name: "wasm class with workers only",
			pool: PoolConfig{Class: PoolClassWASM, Workers: 4},
		},
		{
			name: "wasm class with no sizing",
			pool: PoolConfig{Class: PoolClassWASM},
		},
		{
			name:    "unknown class rejected",
			pool:    PoolConfig{Class: "gpu"},
			wantErr: ErrInvalidPoolClass,
		},
		{
			name:    "negative values still rejected under class",
			pool:    PoolConfig{Class: PoolClassWASM, Workers: -1},
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

func TestFunctionConfigValidateAcceptsWASMClass(t *testing.T) {
	cfg := FunctionConfig{
		FS:     "kickside.convert.doc2md:assets",
		Path:   "doc2md.wasm",
		Hash:   "sha256:deadbeef",
		Method: "convert",
		Pool:   PoolConfig{Class: PoolClassWASM, Workers: 4},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
