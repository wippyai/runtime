// SPDX-License-Identifier: MPL-2.0

package runtime

import "context"

type resultArityKey struct{}

// WithResultArity tells a function adapter how many successful contract values
// precede the outer error. Ordinary function calls retain their legacy shape.
func WithResultArity(ctx context.Context, count int) context.Context {
	if count < 1 {
		count = 1
	}
	return context.WithValue(ctx, resultArityKey{}, count)
}

func ResultArity(ctx context.Context) int {
	if count, ok := ctx.Value(resultArityKey{}).(int); ok && count > 0 {
		return count
	}
	return 1
}
