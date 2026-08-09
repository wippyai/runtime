// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
)

var outdatedNotifierKey = &ctxapi.Key{Name: "process.outdated_notifier"}

// OutdatedNotifier notifies running process instances whose source node (or a
// transitively imported dependency) changed in the registry, so upgradable
// instances can hot-swap via process.upgrade. affected is the set of changed or
// affected source node ids; an instance is matched when its own source node is
// in the set. The per-instance upgradable gate is applied on delivery.
type OutdatedNotifier interface {
	NotifyOutdated(affected map[registry.ID]bool)
}

// WithOutdatedNotifier attaches an OutdatedNotifier to the app context.
func WithOutdatedNotifier(ctx context.Context, n OutdatedNotifier) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(outdatedNotifierKey) == nil {
		ac.With(outdatedNotifierKey, n)
	}
	return ctx
}

// GetOutdatedNotifier retrieves the OutdatedNotifier from the app context.
func GetOutdatedNotifier(ctx context.Context) OutdatedNotifier {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(outdatedNotifierKey); val != nil {
		if n, ok := val.(OutdatedNotifier); ok {
			return n
		}
	}
	return nil
}
