// SPDX-License-Identifier: MPL-2.0

//go:build !excel

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
)

// Excel is a no-op when the runtime is built without the `excel` build tag.
// The excel Lua module and its dependencies are then excluded from the binary.
// Build with `-tags excel` to include them.
func Excel() boot.Component {
	return boot.New(boot.P{
		Name:      ExcelName,
		DependsOn: []boot.Name{EngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			return ctx, nil
		},
	})
}
