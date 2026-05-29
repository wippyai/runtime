// SPDX-License-Identifier: MPL-2.0

//go:build !treesitter

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
)

// TreeSitter is a no-op when the runtime is built without the `treesitter`
// build tag. The treesitter Lua module and its grammar dependencies are then
// excluded from the binary. Build with `-tags treesitter` to include them.
func TreeSitter() boot.Component {
	return boot.New(boot.P{
		Name:      TreeSitterName,
		DependsOn: []boot.Name{EngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			return ctx, nil
		},
	})
}
