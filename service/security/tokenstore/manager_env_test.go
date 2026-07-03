// SPDX-License-Identifier: MPL-2.0

package tokenstore_test

import (
	"context"
	"encoding/json"
	"errors"
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

// realTokenConfigTranscoder unmarshals a Golang map payload into the token store
// config via a JSON round trip so the decode path reads the token_key_env
// directive from the entry data, as it does in production.
type realTokenConfigTranscoder struct{}

func (realTokenConfigTranscoder) Unmarshal(p payload.Payload, v any) error {
	data, ok := p.Data().(map[string]any)
	if !ok {
		return errors.New("payload is not a map")
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (realTokenConfigTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return payload.New(p.Data()), nil
}

func tokenEntry(data map[string]any) registry.Entry {
	return registry.Entry{
		ID:   registry.NewID("app", "sessions"),
		Kind: tokenstore.TokenStore,
		Data: payload.New(data),
	}
}

func TestManager_TokenKeyEnvResolves(t *testing.T) {
	reg := &envRegistryMock{vars: map[string]string{"TOKEN_KEY": "secret-key", "EMPTY_KEY": ""}}
	ctx := envapi.WithRegistry(ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext()), reg)

	t.Run("resolves into TokenKey", func(t *testing.T) {
		entry := tokenEntry(map[string]any{
			"store":         "app:sessions",
			"token_length":  32,
			"token_key_env": "TOKEN_KEY",
		})
		decoded, err := entryutil.DecodeEntryConfig[tokenstore.Config](ctx, realTokenConfigTranscoder{}, entry)
		require.NoError(t, err)
		assert.Equal(t, "secret-key", decoded.TokenKey)
	})

	t.Run("empty env field keeps inline value", func(t *testing.T) {
		entry := tokenEntry(map[string]any{
			"store":         "app:sessions",
			"token_length":  32,
			"token_key":     "inline-key",
			"token_key_env": "EMPTY_KEY",
		})
		decoded, err := entryutil.DecodeEntryConfig[tokenstore.Config](ctx, realTokenConfigTranscoder{}, entry)
		require.NoError(t, err)
		assert.Equal(t, "inline-key", decoded.TokenKey)
	})
}
