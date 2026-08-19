// SPDX-License-Identifier: MPL-2.0

package registry

import "context"

// DependencyAccess controls external dependency access.
type DependencyAccess uint8

const (
	// DependencyAccessUnspecified delegates policy selection to the caller.
	DependencyAccessUnspecified DependencyAccess = iota
	// DependencyAccessOnline permits external resolution and artifact download.
	DependencyAccessOnline
	// DependencyAccessVerifiedOffline forbids external dependency access.
	DependencyAccessVerifiedOffline
)

type dependencyAccessContextKey struct{}

// WithDependencyAccess returns a request-scoped dependency access policy.
func WithDependencyAccess(ctx context.Context, access DependencyAccess) context.Context {
	return context.WithValue(ctx, dependencyAccessContextKey{}, access)
}

// DependencyAccessFromContext returns the request-scoped policy.
func DependencyAccessFromContext(ctx context.Context) DependencyAccess {
	if ctx == nil {
		return DependencyAccessUnspecified
	}
	access, ok := ctx.Value(dependencyAccessContextKey{}).(DependencyAccess)
	if !ok || access > DependencyAccessVerifiedOffline {
		return DependencyAccessUnspecified
	}
	return access
}
