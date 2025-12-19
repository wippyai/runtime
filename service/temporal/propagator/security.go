package propagator

import (
	"context"
	"encoding/json"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	secsystem "github.com/wippyai/runtime/system/security"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
)

// SecurityHeaderKey is the Temporal header key for security context.
const SecurityHeaderKey = "wippy-security"

// SecurityPayload is the JSON-serializable security context for Temporal propagation.
type SecurityPayload struct {
	Actor    *ActorPayload `json:"actor,omitempty"`
	Policies []string      `json:"policies,omitempty"`
}

// ActorPayload is the JSON-serializable actor.
type ActorPayload struct {
	ID   string         `json:"id"`
	Meta map[string]any `json:"meta,omitempty"`
}

// ExtractSecurityPayload extracts security context from Go context for serialization.
// Returns nil if no security context is present.
func ExtractSecurityPayload(ctx context.Context) *SecurityPayload {
	actor, hasActor := secapi.GetActor(ctx)
	scope, hasScope := secapi.GetScope(ctx)

	if !hasActor && !hasScope {
		return nil
	}

	payload := &SecurityPayload{}

	if hasActor {
		payload.Actor = &ActorPayload{
			ID:   actor.ID,
			Meta: actor.Meta,
		}
	}

	if hasScope {
		policies := scope.Policies()
		if len(policies) > 0 {
			payload.Policies = make([]string, 0, len(policies))
			for _, p := range policies {
				id := p.ID()
				payload.Policies = append(payload.Policies, id.String())
			}
		}
	}

	return payload
}

// ApplySecurityPayload applies security context to Go context using existing secapi functions.
// Actor is applied directly. Scope is reconstructed from policy IDs using the registry.
// Returns error only for critical failures; missing policies are skipped.
func ApplySecurityPayload(ctx context.Context, payload *SecurityPayload) error {
	if payload == nil {
		return nil
	}

	// Apply Actor using existing secapi.SetActor
	if payload.Actor != nil {
		actor := secapi.Actor{
			ID:   payload.Actor.ID,
			Meta: attrs.Bag(payload.Actor.Meta),
		}
		if err := secapi.SetActor(ctx, actor); err != nil {
			return err
		}
	}

	// Apply Scope - reconstruct from policy IDs using existing registry
	if len(payload.Policies) > 0 {
		reg, ok := secapi.GetRegistry(ctx)
		if !ok {
			// No registry available - can't reconstruct scope, not an error
			return nil
		}

		var policies []secapi.Policy
		for _, idStr := range payload.Policies {
			id := registry.ParseID(idStr)
			policy, err := reg.GetPolicy(id)
			if err != nil {
				// Skip missing policies - different workers may have different policies
				continue
			}
			policies = append(policies, policy)
		}

		if len(policies) > 0 {
			scope := secsystem.NewScope(policies)
			if err := secapi.SetScope(ctx, scope); err != nil {
				return err
			}
		}
	}

	return nil
}

// ExtractSecurityFromHeader extracts security payload from a Temporal header.
func ExtractSecurityFromHeader(header *commonpb.Header) (*SecurityPayload, error) {
	if header == nil || header.Fields == nil {
		return nil, nil
	}

	p, ok := header.Fields[SecurityHeaderKey]
	if !ok || p == nil {
		return nil, nil
	}

	var jsonBytes []byte
	if err := converter.GetDefaultDataConverter().FromPayload(p, &jsonBytes); err != nil {
		return nil, err
	}

	var payload SecurityPayload
	if err := json.Unmarshal(jsonBytes, &payload); err != nil {
		return nil, err
	}

	return &payload, nil
}

// securityCtxKey is used to pass security payload through Go context (for activities).
type securityCtxKeyType struct{}

var securityCtxKey = securityCtxKeyType{}

// WithSecurityCtx stores security payload in Go context for activity propagation.
func WithSecurityCtx(ctx context.Context, payload *SecurityPayload) context.Context {
	if payload == nil {
		return ctx
	}
	return context.WithValue(ctx, securityCtxKey, payload)
}

// GetSecurityFromCtx retrieves security payload from Go context.
func GetSecurityFromCtx(ctx context.Context) *SecurityPayload {
	if payload, ok := ctx.Value(securityCtxKey).(*SecurityPayload); ok {
		return payload
	}
	return nil
}

// AddSecurityToHeader adds security payload to an existing header (or creates one).
func AddSecurityToHeader(header *commonpb.Header, payload *SecurityPayload) (*commonpb.Header, error) {
	if payload == nil {
		return header, nil
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return header, err
	}

	secPayload, err := converter.GetDefaultDataConverter().ToPayload(jsonBytes)
	if err != nil {
		return header, err
	}

	if header == nil {
		header = &commonpb.Header{Fields: make(map[string]*commonpb.Payload)}
	}
	if header.Fields == nil {
		header.Fields = make(map[string]*commonpb.Payload)
	}

	header.Fields[SecurityHeaderKey] = secPayload
	return header, nil
}
