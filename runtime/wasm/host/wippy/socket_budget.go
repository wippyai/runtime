// SPDX-License-Identifier: MPL-2.0

package wippy

import (
	"context"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type socketBudgetKey struct{}

// WithSocketBudget returns a context carrying the shared actor socket budget.
func WithSocketBudget(ctx context.Context, budget *preview2.SocketBudget) context.Context {
	if budget == nil {
		return ctx
	}
	return context.WithValue(ctx, socketBudgetKey{}, budget)
}

// GetSocketBudget retrieves the shared actor socket budget from the context, or nil if none is set.
func GetSocketBudget(ctx context.Context) *preview2.SocketBudget {
	if ctx == nil {
		return nil
	}
	budget, _ := ctx.Value(socketBudgetKey{}).(*preview2.SocketBudget)
	return budget
}
