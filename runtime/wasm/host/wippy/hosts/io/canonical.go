// SPDX-License-Identifier: MPL-2.0
package io

import (
	"context"
	"errors"

	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func validateOutputWrite(_ context.Context, mod api.Module, stack []uint64) error {
	return validateWriteList(mod, stack, preview2.MaxAllocationSize)
}
func validateBlockingOutputWrite(_ context.Context, mod api.Module, stack []uint64) error {
	return validateWriteList(mod, stack, 4096)
}
func validateWriteList(mod api.Module, stack []uint64, maximum uint32) error {
	if len(stack) < 3 || uint32(stack[2]) > maximum {
		return errors.New("output write exceeds byte limit")
	}
	if mod == nil || mod.Memory() == nil {
		return errors.New("output write memory unavailable")
	}
	if _, ok := mod.Memory().Read(uint32(stack[1]), uint32(stack[2])); !ok {
		return errors.New("output write outside memory")
	}
	return nil
}
