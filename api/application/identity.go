// SPDX-License-Identifier: MPL-2.0

// Package application carries the identity of the root application bundled
// into a native executable. It is independent of the selected deployment.
package application

import "context"

// Identity is the module and version baked into the executable's root pack.
type Identity struct {
	Module  string
	Version string
}

type identityKey struct{}

// WithIdentity makes a bundled application's identity available to runtime code.
func WithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, identity)
}

// FromContext returns the bundled root identity, when one is present.
func FromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityKey{}).(Identity)
	return identity, ok && identity.Module != "" && identity.Version != ""
}
