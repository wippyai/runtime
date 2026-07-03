// SPDX-License-Identifier: MPL-2.0

package tokenstore_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/service/security/tokenstore"
	entryutil "github.com/wippyai/runtime/system/entry"
)

// envRegistryMock is a minimal env.Registry test double.
type envRegistryMock struct {
	vars map[string]string
}

func (m *envRegistryMock) Get(_ context.Context, name string) (string, error) {
	if v, ok := m.vars[name]; ok {
		return v, nil
	}
	return "", envapi.ErrVariableNotFound
}

func (m *envRegistryMock) Lookup(_ context.Context, name string) (string, bool, error) {
	v, ok := m.vars[name]
	return v, ok, nil
}

func (m *envRegistryMock) Set(_ context.Context, name, value string) error {
	m.vars[name] = value
	return nil
}

func (m *envRegistryMock) All(_ context.Context) (map[string]string, error) {
	return m.vars, nil
}

func (m *envRegistryMock) GetStorage(_ context.Context, _ registry.ID) (envapi.Storage, error) {
	return nil, envapi.ErrStorageNotFound
}

func (m *envRegistryMock) RegisterStorage(_ registry.ID, _ envapi.Storage) {}

func (m *envRegistryMock) RegisterVariable(_ envapi.Variable) error { return nil }

func (m *envRegistryMock) UnregisterVariable(_ registry.ID) {}

// staticTokenConfigTranscoder unmarshals a fixed token store config so the test
// drives the decoded struct while exercising the central resolve pass.
type staticTokenConfigTranscoder struct {
	cfg tokenstore.Config
}

func (t *staticTokenConfigTranscoder) Marshal(v any) (payload.Payload, error) {
	return payload.New(v), nil
}

func (t *staticTokenConfigTranscoder) Unmarshal(_ payload.Payload, v any) error {
	target, ok := v.(*tokenstore.Config)
	if !ok {
		return assert.AnError
	}
	*target = t.cfg
	return nil
}

func (t *staticTokenConfigTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	return payload.NewPayload(p.Data(), format), nil
}

func tokenEntry() registry.Entry {
	return registry.Entry{
		ID:   registry.NewID("app", "sessions"),
		Kind: tokenstore.TokenStore,
		Data: payload.New(map[string]string{"test": "data"}),
	}
}

func TestManager_TokenKeyEnvResolves(t *testing.T) {
	reg := &envRegistryMock{vars: map[string]string{"TOKEN_KEY": "secret-key"}}
	ctx := envapi.WithRegistry(ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext()), reg)

	base := tokenstore.Config{Store: registry.NewID("app", "sessions"), TokenLength: 32}

	t.Run("resolves into TokenKey", func(t *testing.T) {
		cfg := base
		cfg.TokenKeyEnv = "TOKEN_KEY"
		decoded, err := entryutil.DecodeEntryConfig[tokenstore.Config](ctx, &staticTokenConfigTranscoder{cfg: cfg}, tokenEntry())
		require.NoError(t, err)
		assert.Equal(t, "secret-key", decoded.TokenKey)
	})

	t.Run("empty env field keeps inline value", func(t *testing.T) {
		cfg := base
		cfg.TokenKey = "inline-key"
		decoded, err := entryutil.DecodeEntryConfig[tokenstore.Config](ctx, &staticTokenConfigTranscoder{cfg: cfg}, tokenEntry())
		require.NoError(t, err)
		assert.Equal(t, "inline-key", decoded.TokenKey)
	})
}
