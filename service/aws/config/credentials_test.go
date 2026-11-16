// SPDX-License-Identifier: MPL-2.0

package config

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/require"
	envapi "github.com/wippyai/runtime/api/env"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	awsconfigapi "github.com/wippyai/runtime/api/service/aws/config"
	syspayload "github.com/wippyai/runtime/system/payload"
	sysjson "github.com/wippyai/runtime/system/payload/json"
)

// withAmbientCredentials exposes credentials to the SDK default chain so a
// test observes any fallback to them.
func withAmbientCredentials(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "ambient-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "ambient-secret")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "missing-shared-credentials")
	t.Setenv("AWS_CONFIG_FILE", "missing-shared-config")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
}

func configEntry(id registry.ID, data map[string]any) registry.Entry {
	return registry.Entry{ID: id, Kind: awsconfigapi.Kind, Data: NewMockPayload(data)}
}

func acquiredCredentials(ctx context.Context, t *testing.T, manager *Manager, id registry.ID) aws.Credentials {
	t.Helper()
	res, err := manager.Acquire(ctx, id, resource.ModeNormal)
	require.NoError(t, err)
	defer res.Release()
	val, err := res.Get()
	require.NoError(t, err)
	creds, err := val.(aws.Config).Credentials.Retrieve(context.Background())
	require.NoError(t, err)
	return creds
}

func requireUnresolvedCredentials(t *testing.T, err error, id registry.ID, field string, secrets ...string) {
	t.Helper()
	require.Error(t, err)
	var apiErr apierror.Error
	require.True(t, errors.As(err, &apiErr))
	require.Equal(t, apierror.Invalid, apiErr.Kind())
	require.Equal(t, apierror.False, apiErr.Retryable())
	require.Contains(t, err.Error(), id.String())
	require.Contains(t, err.Error(), field)
	for _, secret := range secrets {
		require.NotContains(t, err.Error(), secret)
	}
}

func TestManager_StaticCredentials(t *testing.T) {
	withAmbientCredentials(t)
	manager, _, ctx := setupTestEnvironment(t)

	cases := []struct {
		data map[string]any
		name string
	}{
		{name: "inline", data: map[string]any{
			"region": "us-east-1", "access_key_id": "test-access-key", "secret_access_key": "test-secret-key",
		}},
		{name: "env directives", data: map[string]any{
			"region": "us-east-1", "access_key_id_env": "AWS_ACCESS_KEY_ID", "secret_access_key_env": "AWS_SECRET_ACCESS_KEY",
		}},
		{name: "env placeholders", data: map[string]any{
			"region": "us-east-1", "access_key_id": "${env:AWS_ACCESS_KEY_ID}", "secret_access_key": "${env:AWS_SECRET_ACCESS_KEY}",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := registry.NewID("test", "static-"+tc.name)
			require.NoError(t, manager.Add(ctx, configEntry(id, tc.data)))
			creds := acquiredCredentials(ctx, t, manager, id)
			require.Equal(t, "test-access-key", creds.AccessKeyID)
			require.Equal(t, "test-secret-key", creds.SecretAccessKey)
		})
	}
}

func TestManager_UndeclaredCredentialsUseDefaultChain(t *testing.T) {
	withAmbientCredentials(t)
	manager, _, ctx := setupTestEnvironment(t)

	id := registry.NewID("test", "default-chain")
	require.NoError(t, manager.Add(ctx, configEntry(id, map[string]any{"region": "us-east-1"})))
	creds := acquiredCredentials(ctx, t, manager, id)
	require.Equal(t, "ambient-key", creds.AccessKeyID)
}

func TestManager_UnresolvedCredentialsFailClosed(t *testing.T) {
	withAmbientCredentials(t)

	cases := []struct {
		data    map[string]any
		name    string
		field   string
		secrets []string
	}{
		{name: "both directives unbound", field: "access_key_id", data: map[string]any{
			"region": "us-east-1", "access_key_id_env": "UNBOUND_KEY", "secret_access_key_env": "UNBOUND_SECRET",
		}},
		{name: "key directive unbound", field: "access_key_id", secrets: []string{"inline-secret"}, data: map[string]any{
			"region": "us-east-1", "access_key_id_env": "UNBOUND_KEY", "secret_access_key": "inline-secret",
		}},
		{name: "secret directive unbound", field: "secret_access_key", data: map[string]any{
			"region": "us-east-1", "access_key_id_env": "AWS_ACCESS_KEY_ID", "secret_access_key_env": "UNBOUND_SECRET",
		}},
		{name: "key placeholder unbound", field: "access_key_id", secrets: []string{"test-secret-key"}, data: map[string]any{
			"region": "us-east-1", "access_key_id": "${env:UNBOUND_KEY}", "secret_access_key": "${env:AWS_SECRET_ACCESS_KEY}",
		}},
		{name: "secret missing", field: "secret_access_key", data: map[string]any{
			"region": "us-east-1", "access_key_id": "inline-key",
		}},
		{name: "key missing", field: "access_key_id", secrets: []string{"inline-secret"}, data: map[string]any{
			"region": "us-east-1", "secret_access_key": "inline-secret",
		}},
		{name: "secret directive without key", field: "access_key_id", data: map[string]any{
			"region": "us-east-1", "secret_access_key_env": "AWS_SECRET_ACCESS_KEY",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manager, _, ctx := setupTestEnvironment(t)
			id := registry.NewID("test", "unresolved")
			err := manager.Add(ctx, configEntry(id, tc.data))
			requireUnresolvedCredentials(t, err, id, tc.field, tc.secrets...)

			_, err = manager.Acquire(ctx, id, resource.ModeNormal)
			require.Error(t, err)
		})
	}
}

func TestManager_UnresolvedCredentialsRecoverOnReapply(t *testing.T) {
	withAmbientCredentials(t)
	manager, _, ctx := setupTestEnvironment(t)
	bind := func(t *testing.T, key, secret string) {
		t.Helper()
		require.NoError(t, envapi.GetRegistry(ctx).Set(ctx, key, "late-key"))
		require.NoError(t, envapi.GetRegistry(ctx).Set(ctx, secret, "late-secret"))
	}
	directives := func(key, secret string) map[string]any {
		return map[string]any{"region": "us-east-1", "access_key_id_env": key, "secret_access_key_env": secret}
	}

	t.Run("add", func(t *testing.T) {
		id := registry.NewID("test", "late-add")
		data := directives("ADD_KEY", "ADD_SECRET")
		requireUnresolvedCredentials(t, manager.Add(ctx, configEntry(id, data)), id, "access_key_id")

		bind(t, "ADD_KEY", "ADD_SECRET")
		require.NoError(t, manager.Add(ctx, configEntry(id, data)))
		require.Equal(t, "late-key", acquiredCredentials(ctx, t, manager, id).AccessKeyID)
	})

	t.Run("update keeps the last valid credentials", func(t *testing.T) {
		id := registry.NewID("test", "late-update")
		require.NoError(t, manager.Add(ctx, configEntry(id, map[string]any{
			"region": "us-east-1", "access_key_id": "first-key", "secret_access_key": "first-secret",
		})))

		data := directives("UPDATE_KEY", "UPDATE_SECRET")
		requireUnresolvedCredentials(t, manager.Update(ctx, configEntry(id, data)), id, "access_key_id")
		require.Equal(t, "first-key", acquiredCredentials(ctx, t, manager, id).AccessKeyID)

		bind(t, "UPDATE_KEY", "UPDATE_SECRET")
		require.NoError(t, manager.Update(ctx, configEntry(id, data)))
		require.Equal(t, "late-key", acquiredCredentials(ctx, t, manager, id).AccessKeyID)
	})
}

func TestManager_UnresolvedDirectiveFailsWithRuntimeTranscoder(t *testing.T) {
	withAmbientCredentials(t)
	manager, _, ctx := setupTestEnvironment(t)
	transcoder := syspayload.NewTranscoder()
	sysjson.Register(transcoder)
	manager.dtt = transcoder

	for _, data := range []payload.Payload{
		payload.New(map[string]any{"region": "us-east-1", "access_key_id_env": "UNBOUND_KEY", "secret_access_key_env": "UNBOUND_SECRET"}),
		payload.NewPayload(`{"region":"us-east-1","access_key_id_env":"UNBOUND_KEY","secret_access_key_env":"UNBOUND_SECRET"}`, payload.JSON),
	} {
		id := registry.NewID("test", "runtime-transcoder-"+data.Format())
		requireUnresolvedCredentials(t, manager.Add(ctx, registry.Entry{ID: id, Kind: awsconfigapi.Kind, Data: data}), id, "access_key_id")
	}
}
