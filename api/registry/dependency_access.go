// SPDX-License-Identifier: MPL-2.0

package registry

import "context"

// DependencyAccess controls whether a dependency operation may reach the Hub.
//
// The policy is per-operation: one long-lived DependencyHandler serves both
// startup restore, which must stay local, and explicit install/update work,
// which must reach the network. It therefore travels on the operation's
// context rather than the AppContext, whose values are written once during
// boot and sealed, or a FrameContext, which startup paths never open.
type DependencyAccess uint8

const (
	// DependencyAccessVerifiedOffline forbids external dependency access. It is
	// the zero value, so an operation that states no policy stays local.
	DependencyAccessVerifiedOffline DependencyAccess = iota
	// DependencyAccessOnline permits external resolution and artifact download.
	// Callers grant it explicitly for install, update and workspace completion.
	DependencyAccessOnline
)

type dependencyAccessContextKey struct{}

// WithDependencyAccess returns a context carrying the dependency access policy.
func WithDependencyAccess(ctx context.Context, access DependencyAccess) context.Context {
	return context.WithValue(ctx, dependencyAccessContextKey{}, access)
}

// DependencyAccessFromContext returns the policy this operation runs under,
// defaulting to DependencyAccessVerifiedOffline.
func DependencyAccessFromContext(ctx context.Context) DependencyAccess {
	if ctx == nil {
		return DependencyAccessVerifiedOffline
	}
	access, ok := ctx.Value(dependencyAccessContextKey{}).(DependencyAccess)
	if !ok || access > DependencyAccessOnline {
		return DependencyAccessVerifiedOffline
	}
	return access
}

// DependencyDownloadsAllowed reports whether this operation may reach the Hub.
func DependencyDownloadsAllowed(ctx context.Context) bool {
	return DependencyAccessFromContext(ctx) == DependencyAccessOnline
}
