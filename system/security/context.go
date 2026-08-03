// SPDX-License-Identifier: MPL-2.0

package security

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/security"
)

var processSecurityKey = &ctxapi.Key{Name: "security.process_entry", Inherit: true}

type processSecurityState struct {
	scope      security.Scope
	actor      security.Actor
	hasActor   bool
	hasScope   bool
	configured bool
}

func actorConfigured(actor security.Actor) bool {
	return actor.ID != "" || len(actor.Meta) > 0
}

func HasProcessSecurityConfig(ctx context.Context) bool {
	frame := ctxapi.FrameFromContext(ctx)
	if frame == nil {
		return false
	}
	state, ok := frame.Get(processSecurityKey)
	if !ok {
		return false
	}
	entryState, ok := state.(processSecurityState)
	return ok && entryState.configured
}

func ApplyProcessSecurityConfig(ctx context.Context, config *security.Config) context.Context {
	frame := ctxapi.FrameFromContext(ctx)
	if frame == nil {
		return ctx
	}

	stateValue, ok := frame.Get(processSecurityKey)
	state, hasState := stateValue.(processSecurityState)
	if !ok || !hasState {
		state.actor, state.hasActor = security.GetActor(ctx)
		state.scope, state.hasScope = security.GetScope(ctx)
	} else {
		if state.hasActor {
			if err := security.SetActor(ctx, state.actor); err != nil {
				return ctx
			}
		} else if err := security.SetActor(ctx, security.Actor{}); err != nil {
			return ctx
		}
		if state.hasScope {
			if err := security.SetScope(ctx, state.scope); err != nil {
				return ctx
			}
		} else if err := security.SetScope(ctx, NewScope(nil)); err != nil {
			return ctx
		}
	}

	if config != nil {
		ctx = WithSecurityConfig(ctx, config)
		state.configured = true
	} else {
		state.configured = false
	}
	if err := frame.Set(processSecurityKey, state); err != nil {
		return ctx
	}
	return ctx
}

// WithSecurityConfig configures the security context based on the provided configuration.
func WithSecurityConfig(ctx context.Context, config *security.Config) context.Context {
	if config == nil {
		return ctx
	}

	if actorConfigured(config.Actor) {
		if err := security.SetActor(ctx, config.Actor); err != nil {
			return ctx
		}
	} else if _, ok := security.GetActor(ctx); !ok {
		if err := security.SetActor(ctx, config.Actor); err != nil {
			return ctx
		}
	}

	reg, ok := security.GetRegistry(ctx)
	if !ok {
		return ctx
	}

	allPolicies := make([]security.Policy, 0)

	for _, groupID := range config.PolicyGroups {
		groupScope, err := reg.GetPolicyGroup(groupID)
		if err == nil {
			allPolicies = append(allPolicies, groupScope.Policies()...)
		}
	}

	for _, policyID := range config.Policies {
		policy, err := reg.GetPolicy(policyID)
		if err == nil {
			allPolicies = append(allPolicies, policy)
		}
	}

	if len(allPolicies) > 0 {
		scope := NewScope(allPolicies)
		if existing, ok := security.GetScope(ctx); ok && existing != nil {
			scope = existing
			for _, policy := range allPolicies {
				scope = scope.With(policy)
			}
		}
		if err := security.SetScope(ctx, scope); err != nil {
			return ctx
		}
	}

	return ctx
}
