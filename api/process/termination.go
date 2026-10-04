// SPDX-License-Identifier: MPL-2.0

package process

import "context"

type terminationCauseKey struct{}

// WithTerminationCause sets the completion error requested by a Terminate call.
// Managers forward this context to the host; actor hosts cancel the target with
// this cause. It does not cancel ctx or change the error returned by Terminate.
func WithTerminationCause(ctx context.Context, cause error) context.Context {
	return context.WithValue(ctx, terminationCauseKey{}, cause)
}

// TerminationCause returns the completion error requested by Terminate, or nil
// for the host's default explicit termination error.
func TerminationCause(ctx context.Context) error {
	cause, _ := ctx.Value(terminationCauseKey{}).(error)
	return cause
}
