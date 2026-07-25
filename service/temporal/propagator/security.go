// SPDX-License-Identifier: MPL-2.0

package propagator

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash"
	"sort"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	secsystem "github.com/wippyai/runtime/system/security"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
)

// SecurityHeaderKey is the Temporal header key for security context.
const (
	SecurityHeaderKey          = "wippy-security"
	SecuritySignatureHeaderKey = "wippy-security-signature"
	minimumSecurityKeySize     = 32
)

// SecurityPayload is the JSON-serializable security context for Temporal propagation.
type SecurityPayload struct {
	Actor    *ActorPayload `json:"actor,omitempty"`
	Audience string        `json:"audience"`
	Policies []string      `json:"policies,omitempty"`
	Scope    bool          `json:"scope,omitempty"`
}

// ActorPayload is the JSON-serializable actor.
type ActorPayload struct {
	Meta map[string]any `json:"meta,omitempty"`
	ID   string         `json:"id"`
}

// ExtractSecurityPayload extracts security context from Go context for serialization.
// Returns nil if no security context is present.
func ExtractSecurityPayload(ctx context.Context) *SecurityPayload {
	actor, hasActor := secapi.GetActor(ctx)
	scope, hasScope := secapi.GetScope(ctx)

	if !hasActor && !hasScope {
		return nil
	}

	payload := &SecurityPayload{Audience: GetSecurityAudience(ctx)}

	if hasActor {
		payload.Actor = &ActorPayload{
			ID:   actor.ID,
			Meta: actor.Meta,
		}
	}

	if hasScope {
		payload.Scope = true
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

// ApplySecurityPayload installs a verified execution claim into a frame.
func ApplySecurityPayload(ctx context.Context, payload *SecurityPayload) error {
	if payload == nil {
		return nil
	}
	if (payload.Actor != nil) != payload.Scope {
		return fmt.Errorf("incomplete temporal security context")
	}
	if payload.Actor == nil {
		if len(payload.Policies) > 0 {
			return fmt.Errorf("security policies require an actor and scope")
		}
		return nil
	}
	if payload.Actor.ID == "" {
		return fmt.Errorf("security actor ID is required")
	}

	policies := make([]secapi.Policy, 0, len(payload.Policies))
	if len(payload.Policies) > 0 {
		reg, ok := secapi.GetRegistry(ctx)
		if !ok {
			return fmt.Errorf("security registry not available")
		}
		for _, idStr := range payload.Policies {
			id := registry.ParseID(idStr)
			if id.NS == "" || id.Name == "" {
				return fmt.Errorf("invalid security policy ID %q", idStr)
			}
			policy, err := reg.GetPolicy(id)
			if err != nil {
				return fmt.Errorf("resolve security policy %q: %w", idStr, err)
			}
			policies = append(policies, policy)
		}
	}

	frame := ctxapi.FrameFromContext(ctx)
	if frame == nil {
		return secapi.ErrNoFrameContext
	}
	actor := secapi.Actor{ID: payload.Actor.ID, Meta: attrs.Bag(payload.Actor.Meta)}
	return frame.SetMultiple(
		secapi.ActorPair(actor),
		secapi.ScopePair(secsystem.NewScope(policies)),
	)
}

func resolveSecurityKeys(keys [][]byte) ([][]byte, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("temporal security HMAC key must be at least %d bytes", minimumSecurityKeySize)
	}
	resolved := make([][]byte, len(keys))
	for i, key := range keys {
		if len(key) < minimumSecurityKeySize {
			return nil, fmt.Errorf("temporal security HMAC key must be at least %d bytes", minimumSecurityKeySize)
		}
		resolved[i] = key
	}
	return resolved, nil
}

func writeSecurityMACPart(mac hash.Hash, data []byte) {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(data)))
	_, _ = mac.Write(size[:])
	_, _ = mac.Write(data)
}

func securityPayloadMAC(key []byte, payload *commonpb.Payload) []byte {
	return temporalPayloadMAC("wippy/temporal/security/v1", key, payload)
}

func temporalPayloadMAC(domain string, key []byte, payload *commonpb.Payload) []byte {
	mac := hmac.New(sha256.New, key)
	writeSecurityMACPart(mac, []byte(domain))
	keys := make([]string, 0, len(payload.Metadata))
	for name := range payload.Metadata {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		writeSecurityMACPart(mac, []byte(name))
		writeSecurityMACPart(mac, payload.Metadata[name])
	}
	writeSecurityMACPart(mac, payload.Data)
	return mac.Sum(nil)
}

// Security envelopes use a fixed JSON wire format so their identity and
// meaning cannot change with an application's registered payload graph.
func encodeSecurityEnvelope(value any) (*commonpb.Payload, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &commonpb.Payload{
		Metadata: map[string][]byte{
			converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
		},
		Data: data,
	}, nil
}

func decodeSecurityEnvelope(payload *commonpb.Payload, value any) error {
	if payload == nil || string(payload.Metadata[converter.MetadataEncoding]) != converter.MetadataEncodingJSON {
		return fmt.Errorf("invalid temporal security envelope encoding")
	}
	return json.Unmarshal(payload.Data, value)
}

// ExtractSecurityFromHeader extracts security payload from a Temporal header.
func ExtractSecurityFromHeader(dc converter.DataConverter, header *commonpb.Header, expectedAudience string, keys ...[]byte) (*SecurityPayload, error) {
	if dc == nil {
		return nil, fmt.Errorf("data converter not available")
	}
	if header == nil || header.Fields == nil {
		return nil, nil
	}

	payload, hasPayload := header.Fields[SecurityHeaderKey]
	signature, hasSignature := header.Fields[SecuritySignatureHeaderKey]
	if !hasPayload && !hasSignature {
		return nil, nil
	}
	if !hasPayload || payload == nil || !hasSignature || signature == nil {
		return nil, fmt.Errorf("incomplete temporal security header")
	}
	resolvedKeys, err := resolveSecurityKeys(keys)
	if err != nil {
		return nil, err
	}
	validSignature := false
	if len(signature.Data) == sha256.Size {
		for _, key := range resolvedKeys {
			if hmac.Equal(signature.Data, securityPayloadMAC(key, payload)) {
				validSignature = true
				break
			}
		}
	}
	if !validSignature {
		return nil, fmt.Errorf("invalid temporal security signature")
	}

	var securityPayload SecurityPayload
	if err := decodeSecurityEnvelope(payload, &securityPayload); err != nil {
		return nil, err
	}
	if expectedAudience == "" || securityPayload.Audience == "" || securityPayload.Audience != expectedAudience {
		return nil, fmt.Errorf("invalid temporal security audience")
	}
	return &securityPayload, nil
}

// securityCtxKey is used to pass security payload through Go context (for activities).
type securityCtxKeyType struct{}
type securityAudienceType struct{}

var securityCtxKey = securityCtxKeyType{}
var securityAudienceKey = securityAudienceType{}

func WithSecurityAudience(ctx context.Context, audience string) context.Context {
	return context.WithValue(ctx, securityAudienceKey, audience)
}

func GetSecurityAudience(ctx context.Context) string {
	audience, _ := ctx.Value(securityAudienceKey).(string)
	return audience
}

// WithSecurityCtx stores security payload in Go context for activity propagation.
func WithSecurityCtx(ctx context.Context, payload *SecurityPayload) context.Context {
	if payload == nil {
		return ctx
	}
	ctx = context.WithValue(ctx, securityCtxKey, payload)
	return WithSecurityAudience(ctx, payload.Audience)
}

// GetSecurityFromCtx retrieves security payload from Go context.
func GetSecurityFromCtx(ctx context.Context) *SecurityPayload {
	if payload, ok := ctx.Value(securityCtxKey).(*SecurityPayload); ok {
		return payload
	}
	return nil
}

// AddSecurityToHeader adds security payload to an existing header (or creates one).
func AddSecurityToHeader(dc converter.DataConverter, header *commonpb.Header, payload *SecurityPayload, keys ...[]byte) (*commonpb.Header, error) {
	if dc == nil {
		return header, fmt.Errorf("data converter not available")
	}
	if payload == nil {
		return header, nil
	}
	if payload.Audience == "" {
		return header, fmt.Errorf("temporal security audience is required")
	}
	resolvedKeys, err := resolveSecurityKeys(keys)
	if err != nil {
		return header, err
	}

	encoded, err := encodeSecurityEnvelope(payload)
	if err != nil {
		return header, err
	}

	if header == nil {
		header = &commonpb.Header{Fields: make(map[string]*commonpb.Payload)}
	}
	if header.Fields == nil {
		header.Fields = make(map[string]*commonpb.Payload)
	}

	header.Fields[SecurityHeaderKey] = encoded
	header.Fields[SecuritySignatureHeaderKey] = &commonpb.Payload{Data: securityPayloadMAC(resolvedKeys[0], encoded)}
	return header, nil
}
