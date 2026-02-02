package policy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	policyapi "github.com/wippyai/runtime/api/service/security/policy"
)

// mockTranscoder implements payload.Transcoder for testing
type mockTranscoder struct{}

func (m *mockTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	return payload.NewPayload(p, format), nil
}

func (m *mockTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
	switch dest := v.(type) {
	case *policyapi.Config:
		if src, ok := p.Data().(*policyapi.Config); ok {
			*dest = *src
			return nil
		}
	case *policyapi.ExprConfig:
		if src, ok := p.Data().(*policyapi.ExprConfig); ok {
			*dest = *src
			return nil
		}
	}
	return nil
}

func TestDefaultFactory_CreateConditionPolicy(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{}
	factory := NewDefaultFactory(dtt)

	config := &policyapi.Config{
		Policy: policyapi.Definition{
			Actions:   "*",
			Resources: "*",
			Effect:    policyapi.Allow,
		},
		Groups: []string{"admin"},
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "policy1"),
		Kind: policyapi.Policy,
		Data: payload.New(config),
	}

	policyEntry, err := factory.CreatePolicyEntry(ctx, entry)
	require.NoError(t, err)
	require.NotNil(t, policyEntry)
	assert.NotNil(t, policyEntry.Policy)
	assert.Equal(t, entry.ID, policyEntry.Policy.ID())
	assert.Len(t, policyEntry.Groups, 1)
	assert.Equal(t, registry.NewID("test", "admin"), policyEntry.Groups[0])
}

func TestDefaultFactory_CreateExprPolicy(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{}
	factory := NewDefaultFactory(dtt)

	config := &policyapi.ExprConfig{
		Policy: policyapi.ExprDefinition{
			Actions:    "*",
			Resources:  "*",
			Expression: "true",
			Effect:     policyapi.Allow,
		},
		Groups: []string{"user"},
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "expr1"),
		Kind: policyapi.ExprKind,
		Data: payload.New(config),
	}

	policyEntry, err := factory.CreatePolicyEntry(ctx, entry)
	require.NoError(t, err)
	require.NotNil(t, policyEntry)
	assert.NotNil(t, policyEntry.Policy)
	assert.Equal(t, entry.ID, policyEntry.Policy.ID())
	assert.Len(t, policyEntry.Groups, 1)
	assert.Equal(t, registry.NewID("test", "user"), policyEntry.Groups[0])
}

func TestDefaultFactory_UnsupportedKind(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{}
	factory := NewDefaultFactory(dtt)

	entry := registry.Entry{
		ID:   registry.NewID("test", "other"),
		Kind: "unsupported.kind",
		Data: payload.New(map[string]any{}),
	}

	policyEntry, err := factory.CreatePolicyEntry(ctx, entry)
	apiErr := requireAPIError(t, err, apierror.Invalid, "unsupported policy kind")
	assertDetailString(t, apiErr, "kind", "unsupported.kind")
	assert.Nil(t, policyEntry)
}

func TestDefaultFactory_InvalidConditionPolicyConfig(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{}
	factory := NewDefaultFactory(dtt)

	config := &policyapi.Config{
		Policy: policyapi.Definition{
			Actions:   "",
			Resources: "",
			Effect:    policyapi.Allow,
		},
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "invalid"),
		Kind: policyapi.Policy,
		Data: payload.New(config),
	}

	policyEntry, err := factory.CreatePolicyEntry(ctx, entry)
	require.Error(t, err)
	assert.Nil(t, policyEntry)
}

func TestDefaultFactory_InvalidExprPolicyConfig(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{}
	factory := NewDefaultFactory(dtt)

	config := &policyapi.ExprConfig{
		Policy: policyapi.ExprDefinition{
			Actions:    "*",
			Resources:  "*",
			Expression: "",
			Effect:     policyapi.Allow,
		},
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "invalid-expr"),
		Kind: policyapi.ExprKind,
		Data: payload.New(config),
	}

	policyEntry, err := factory.CreatePolicyEntry(ctx, entry)
	apiErr := requireAPIError(t, err, apierror.Invalid, "failed to decode expr policy config")
	assert.Contains(t, apiErr.Details().GetString("cause", ""), policyapi.ErrExpressionEmpty.Error())
	assert.Nil(t, policyEntry)
}

func TestDefaultFactory_MultipleGroups(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{}
	factory := NewDefaultFactory(dtt)

	config := &policyapi.Config{
		Policy: policyapi.Definition{
			Actions:   "*",
			Resources: "*",
			Effect:    policyapi.Allow,
		},
		Groups: []string{"admin", "moderator", "user"},
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "multi-group"),
		Kind: policyapi.Policy,
		Data: payload.New(config),
	}

	policyEntry, err := factory.CreatePolicyEntry(ctx, entry)
	require.NoError(t, err)
	require.NotNil(t, policyEntry)
	assert.Len(t, policyEntry.Groups, 3)
	assert.Equal(t, registry.NewID("test", "admin"), policyEntry.Groups[0])
	assert.Equal(t, registry.NewID("test", "moderator"), policyEntry.Groups[1])
	assert.Equal(t, registry.NewID("test", "user"), policyEntry.Groups[2])
}
