// SPDX-License-Identifier: MPL-2.0

package config

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	envapi "github.com/wippyai/runtime/api/env"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	awsconfigapi "github.com/wippyai/runtime/api/service/aws/config"
	entryutil "github.com/wippyai/runtime/system/entry"
	"github.com/wippyai/runtime/system/eventbus"
	systemresource "github.com/wippyai/runtime/system/resource"
	"go.uber.org/zap"
)

// NewMockPayload wraps a raw data map so the central *_env decode pass reads
// directives from the entry data, as it does in production.
func NewMockPayload(data map[string]any) payload.Payload {
	return payload.New(data)
}

// MockTranscoder unmarshals a Golang map payload into the config struct via a
// JSON round trip so the decode path exercises real type coercion. An injected
// unmarshalError forces the failure branch.
type MockTranscoder struct {
	unmarshalError error
}

func NewMockTranscoder() *MockTranscoder {
	return &MockTranscoder{}
}

func (m *MockTranscoder) Unmarshal(p payload.Payload, v any) error {
	if m.unmarshalError != nil {
		return m.unmarshalError
	}
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

func (m *MockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return payload.New(p.Data()), nil
}

// MockEnvRegistry implements envapi.Registry for testing
type MockEnvRegistry struct {
	variables map[string]string
}

func NewMockRegistry() *MockEnvRegistry {
	return &MockEnvRegistry{
		variables: make(map[string]string),
	}
}

func (m *MockEnvRegistry) Get(_ context.Context, name string) (string, error) {
	if value, exists := m.variables[name]; exists {
		return value, nil
	}
	return "", envapi.ErrVariableNotFound
}

func (m *MockEnvRegistry) GetFromStorage(_ context.Context, name string) (string, error) {
	if value, exists := m.variables[name]; exists {
		return value, nil
	}
	return "", envapi.ErrVariableNotFound
}

func (m *MockEnvRegistry) Set(_ context.Context, name string, value string) error {
	m.variables[name] = value
	return nil
}

func (m *MockEnvRegistry) All(_ context.Context) (map[string]string, error) {
	return m.variables, nil
}

func (m *MockEnvRegistry) Lookup(_ context.Context, name string) (string, bool, error) {
	if value, exists := m.variables[name]; exists {
		return value, true, nil
	}
	return "", false, nil
}

func (m *MockEnvRegistry) GetStorage(_ context.Context, _ registry.ID) (envapi.Storage, error) {
	return nil, envapi.ErrStorageNotFound
}

func (m *MockEnvRegistry) RegisterStorage(_ registry.ID, _ envapi.Storage) {}

func (m *MockEnvRegistry) RegisterVariable(_ envapi.Variable) error { return nil }

func (m *MockEnvRegistry) UnregisterVariable(_ registry.ID) {}

// setupTestEnvironment creates a test environment with mocked dependencies
func setupTestEnvironment(t *testing.T) (*Manager, event.Bus, context.Context) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	transcoder := NewMockTranscoder()

	envRegistry := NewMockRegistry()

	// Context carries the env registry so the central decode pass resolves every
	// *_env directive (region, access key, secret key) from the entry data.
	ctx := envapi.WithRegistry(ctxapi.NewRootContext(), envRegistry)

	require.NoError(t, envRegistry.Set(ctx, "AWS_ACCESS_KEY_ID", "test-access-key"))
	require.NoError(t, envRegistry.Set(ctx, "AWS_SECRET_ACCESS_KEY", "test-secret-key"))
	require.NoError(t, envRegistry.Set(ctx, "AWS_REGION", "eu-west-1"))

	manager := NewManager(bus, transcoder, logger)

	return manager, bus, ctx
}

// setupResourceEventsListener sets up a listener for resource events
func setupResourceEventsListener(ctx context.Context, bus event.Bus) (chan event.Event, func(), error) {
	resourceEvents := make(chan event.Event, 1)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		resource.System,
		"", // Any kind
		func(evt event.Event) {
			resourceEvents <- evt
		},
	)
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() {
		sub.Close()
	}

	return resourceEvents, cleanup, nil
}

// waitForResourceEvent waits for a resource event with the specified kind
func waitForResourceEvent(t *testing.T, eventChan chan event.Event, expectedKind event.Kind, timeout time.Duration) event.Event {
	t.Helper()

	select {
	case evt := <-eventChan:
		assert.Equal(t, expectedKind, evt.Kind)
		return evt
	case <-time.After(timeout):
		t.Fatalf("timeout waiting for %s event", expectedKind)
		return event.Event{} // Never reached, just to satisfy compiler
	}
}

func TestManager_Add(t *testing.T) {
	manager, bus, ctx := setupTestEnvironment(t)

	// Set up event listener for resource events
	resourceEvents, cleanup, err := setupResourceEventsListener(ctx, bus)
	require.NoError(t, err)
	defer cleanup()

	testID := registry.NewID("test", "awsconfig")

	t.Run("successful config addition", func(t *testing.T) {
		entry := registry.Entry{
			ID:   testID,
			Kind: awsconfigapi.Kind,
			Data: NewMockPayload(map[string]any{
				"region":                "us-east-1",
				"access_key_id_env":     "AWS_ACCESS_KEY_ID",
				"secret_access_key_env": "AWS_SECRET_ACCESS_KEY",
			}),
		}

		err := manager.Add(ctx, entry)
		require.NoError(t, err)

		// Verify config was added to the manager's map
		config, exists := manager.configs[testID]
		assert.True(t, exists)
		assert.NotNil(t, config)
		assert.Equal(t, "us-east-1", config.Region)

		// Verify resource registration event was sent
		evt := waitForResourceEvent(t, resourceEvents, resource.Register, time.Second)
		assert.Equal(t, testID.String(), evt.Path)

		// Verify event data
		resourceEntry, ok := evt.Data.(resource.Entry)
		assert.True(t, ok)
		assert.Equal(t, manager, resourceEntry.Provider)

		// Verify metadata
		meta := resourceEntry.Meta
		assert.Equal(t, "us-east-1", meta["region"])

		// Verify resolved credentials reach the AWS credentials provider.
		res, err := manager.Acquire(ctx, testID, resource.ModeNormal)
		require.NoError(t, err)
		val, err := res.Get()
		require.NoError(t, err)
		awsCfg, ok := val.(aws.Config)
		require.True(t, ok)
		creds, err := awsCfg.Credentials.Retrieve(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "test-access-key", creds.AccessKeyID)
		assert.Equal(t, "test-secret-key", creds.SecretAccessKey)
	})

	t.Run("successful config addition with region env", func(t *testing.T) {
		envID := registry.NewID("test", "awsconfig-region-env")
		entry := registry.Entry{
			ID:   envID,
			Kind: awsconfigapi.Kind,
			Data: NewMockPayload(map[string]any{
				"region_env":            "AWS_REGION",
				"access_key_id_env":     "AWS_ACCESS_KEY_ID",
				"secret_access_key_env": "AWS_SECRET_ACCESS_KEY",
			}),
		}

		err := manager.Add(ctx, entry)
		require.NoError(t, err)

		config, exists := manager.configs[envID]
		assert.True(t, exists)
		assert.Equal(t, "eu-west-1", config.Region)

		evt := waitForResourceEvent(t, resourceEvents, resource.Register, time.Second)
		assert.Equal(t, envID.String(), evt.Path)

		resourceEntry, ok := evt.Data.(resource.Entry)
		require.True(t, ok)
		assert.Equal(t, "eu-west-1", resourceEntry.Meta["region"])
	})

	t.Run("wrong entry kind", func(t *testing.T) {
		entry := registry.Entry{
			Kind: "invalid.kind",
			Data: NewMockPayload(map[string]any{"region": "us-east-1"}),
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported entry kind")
		var apiErr apierror.Error
		ok := errors.As(err, &apiErr)
		assert.True(t, ok)
		kind, _ := apiErr.Details().Get("kind")
		assert.Equal(t, "invalid.kind", kind)
	})

	t.Run("unmarshal error", func(t *testing.T) {
		// Configure transcoder to return error
		manager.dtt = &MockTranscoder{unmarshalError: errors.New("unmarshal error")}

		entry := registry.Entry{
			Kind: awsconfigapi.Kind,
			Data: NewMockPayload(map[string]any{"region": "us-east-1"}),
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "decode config")
		var apiErr apierror.Error
		ok := errors.As(err, &apiErr)
		assert.True(t, ok)
		cause, _ := apiErr.Details().Get("cause")
		assert.Contains(t, cause, "unmarshal error")

		// Reset transcoder for other tests
		manager.dtt = NewMockTranscoder()
	})

	t.Run("duplicate config", func(t *testing.T) {
		entry := registry.Entry{
			ID:   testID, // Same ID as in successful test
			Kind: awsconfigapi.Kind,
			Data: NewMockPayload(map[string]any{"region": "us-east-1"}),
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
		var apiErr apierror.Error
		ok := errors.As(err, &apiErr)
		assert.True(t, ok)
		id, _ := apiErr.Details().Get("id")
		assert.Equal(t, testID.String(), id)
	})
}

func TestManager_Update(t *testing.T) {
	manager, bus, ctx := setupTestEnvironment(t)

	// Set up event listener for resource events
	resourceEvents, cleanup, err := setupResourceEventsListener(ctx, bus)
	require.NoError(t, err)
	defer cleanup()

	testID := registry.NewID("test", "awsconfig")

	// First add a config
	addEntry := registry.Entry{
		ID:   testID,
		Kind: awsconfigapi.Kind,
		Data: NewMockPayload(map[string]any{
			"region":                "us-east-1",
			"access_key_id_env":     "AWS_ACCESS_KEY_ID",
			"secret_access_key_env": "AWS_SECRET_ACCESS_KEY",
		}),
	}

	err = manager.Add(ctx, addEntry)
	require.NoError(t, err)

	// Drain the add event
	waitForResourceEvent(t, resourceEvents, resource.Register, time.Second)

	t.Run("successful update", func(t *testing.T) {
		// Create update entry with the same ID but different region
		updateEntry := registry.Entry{
			ID:   testID,
			Kind: awsconfigapi.Kind,
			Data: NewMockPayload(map[string]any{
				"region":                "us-west-2",
				"access_key_id_env":     "AWS_ACCESS_KEY_ID",
				"secret_access_key_env": "AWS_SECRET_ACCESS_KEY",
			}),
		}

		// Update the config
		err := manager.Update(ctx, updateEntry)
		require.NoError(t, err)

		// Verify config was updated in the manager's map
		manager.mu.RLock()
		config, exists := manager.configs[testID]
		manager.mu.RUnlock()

		assert.True(t, exists)
		assert.Equal(t, "us-west-2", config.Region)

		// Verify resource update event was sent
		evt := waitForResourceEvent(t, resourceEvents, resource.Update, time.Second)
		assert.Equal(t, testID.String(), evt.Path)

		// Verify event data
		resourceEntry, ok := evt.Data.(resource.Entry)
		assert.True(t, ok)

		// Verify updated metadata
		meta := resourceEntry.Meta
		assert.Equal(t, "us-west-2", meta["region"])
	})

	t.Run("config not found", func(t *testing.T) {
		nonExistentID := registry.NewID("test", "nonexistent")
		entry := registry.Entry{
			ID:   nonExistentID,
			Kind: awsconfigapi.Kind,
			Data: NewMockPayload(map[string]any{"region": "us-east-1"}),
		}

		err := manager.Update(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		var apiErr apierror.Error
		ok := errors.As(err, &apiErr)
		assert.True(t, ok)
		id, _ := apiErr.Details().Get("id")
		assert.Equal(t, nonExistentID.String(), id)
	})

	t.Run("wrong entry kind", func(t *testing.T) {
		entry := registry.Entry{
			ID:   testID,
			Kind: "invalid.kind",
			Data: NewMockPayload(map[string]any{}),
		}

		err := manager.Update(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported entry kind")
		var apiErr apierror.Error
		ok := errors.As(err, &apiErr)
		assert.True(t, ok)
		kind, _ := apiErr.Details().Get("kind")
		assert.Equal(t, "invalid.kind", kind)
	})

	t.Run("unmarshal error", func(t *testing.T) {
		// Configure transcoder to return error
		manager.dtt = &MockTranscoder{unmarshalError: errors.New("unmarshal error")}

		entry := registry.Entry{
			ID:   testID,
			Kind: awsconfigapi.Kind,
			Data: NewMockPayload(map[string]any{"region": "us-east-1"}),
		}

		err := manager.Update(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "decode config")
		var apiErr apierror.Error
		ok := errors.As(err, &apiErr)
		assert.True(t, ok)
		cause, _ := apiErr.Details().Get("cause")
		assert.Contains(t, cause, "unmarshal error")

		// Reset transcoder for other tests
		manager.dtt = NewMockTranscoder()
	})
}

func TestManager_Delete(t *testing.T) {
	manager, bus, ctx := setupTestEnvironment(t)

	// Set up event listener for resource events
	resourceEvents, cleanup, err := setupResourceEventsListener(ctx, bus)
	require.NoError(t, err)
	defer cleanup()

	testID := registry.NewID("test", "awsconfig")

	// First add a config
	addEntry := registry.Entry{
		ID:   testID,
		Kind: awsconfigapi.Kind,
		Data: NewMockPayload(map[string]any{
			"region":                "us-east-1",
			"access_key_id_env":     "AWS_ACCESS_KEY_ID",
			"secret_access_key_env": "AWS_SECRET_ACCESS_KEY",
		}),
	}

	err = manager.Add(ctx, addEntry)
	require.NoError(t, err)

	// Drain the add event
	waitForResourceEvent(t, resourceEvents, resource.Register, time.Second)

	t.Run("successful deletion", func(t *testing.T) {
		// Delete the config
		err := manager.Delete(ctx, addEntry)
		require.NoError(t, err)

		// Verify config was removed from the manager's map
		manager.mu.RLock()
		_, exists := manager.configs[testID]
		manager.mu.RUnlock()
		assert.False(t, exists)

		// Verify resource delete event was sent
		evt := waitForResourceEvent(t, resourceEvents, resource.Delete, time.Second)
		assert.Equal(t, testID.String(), evt.Path)

		// Verify event data contains the ID
		id, ok := evt.Data.(registry.ID)
		assert.True(t, ok)
		assert.Equal(t, testID, id)
	})

	t.Run("config not found", func(t *testing.T) {
		// Try to delete again (should fail as already deleted)
		err := manager.Delete(ctx, addEntry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		var apiErr apierror.Error
		ok := errors.As(err, &apiErr)
		assert.True(t, ok)
		id, _ := apiErr.Details().Get("id")
		assert.Equal(t, testID.String(), id)
	})

	t.Run("wrong entry kind", func(t *testing.T) {
		entry := registry.Entry{
			ID:   testID,
			Kind: "invalid.kind",
			Data: NewMockPayload(map[string]any{}),
		}

		err := manager.Delete(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported entry kind")
		var apiErr apierror.Error
		ok := errors.As(err, &apiErr)
		assert.True(t, ok)
		kind, _ := apiErr.Details().Get("kind")
		assert.Equal(t, "invalid.kind", kind)
	})
}

func TestManager_Acquire(t *testing.T) {
	manager, _, ctx := setupTestEnvironment(t)

	testID := registry.NewID("test", "awsconfig")

	// Add a config first
	addEntry := registry.Entry{
		ID:   testID,
		Kind: awsconfigapi.Kind,
		Data: NewMockPayload(map[string]any{
			"region":                "us-east-1",
			"access_key_id_env":     "AWS_ACCESS_KEY_ID",
			"secret_access_key_env": "AWS_SECRET_ACCESS_KEY",
		}),
	}

	err := manager.Add(ctx, addEntry)
	require.NoError(t, err)

	t.Run("successful acquisition", func(t *testing.T) {
		// Acquire the resource
		res, err := manager.Acquire(ctx, testID, resource.ModeNormal)
		require.NoError(t, err)
		require.NotNil(t, res)

		// Get the resource value
		val, err := res.Get()
		require.NoError(t, err)

		// Verify the resource is an AWS config
		config, ok := val.(aws.Config)
		assert.True(t, ok)
		assert.Equal(t, "us-east-1", config.Region)
	})

	t.Run("resource not found", func(t *testing.T) {
		nonExistentID := registry.NewID("test", "nonexistent")

		// Try to acquire a non-existent resource
		res, err := manager.Acquire(ctx, nonExistentID, resource.ModeNormal)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		var apiErr apierror.Error
		ok := errors.As(err, &apiErr)
		assert.True(t, ok)
		id, _ := apiErr.Details().Get("id")
		assert.Equal(t, nonExistentID.String(), id)
		assert.Nil(t, res)
	})

	t.Run("unsupported access mode", func(t *testing.T) {
		// Try to acquire with an unsupported mode
		res, err := manager.Acquire(ctx, testID, resource.ModeExclusive)
		assert.Error(t, err)
		assert.Equal(t, systemresource.ErrLocked, err)
		assert.Nil(t, res)
	})
}

func TestConfigResource(t *testing.T) {
	manager, _, ctx := setupTestEnvironment(t)

	testID := registry.NewID("test", "awsconfig")

	// Add a config first
	addEntry := registry.Entry{
		ID:   testID,
		Kind: awsconfigapi.Kind,
		Data: NewMockPayload(map[string]any{
			"region":                "us-east-1",
			"access_key_id_env":     "AWS_ACCESS_KEY_ID",
			"secret_access_key_env": "AWS_SECRET_ACCESS_KEY",
		}),
	}

	err := manager.Add(ctx, addEntry)
	require.NoError(t, err)

	// Acquire the resource
	res, err := manager.Acquire(ctx, testID, resource.ModeNormal)
	require.NoError(t, err)
	require.NotNil(t, res)

	t.Run("get config", func(t *testing.T) {
		// Get the resource value
		val, err := res.Get()
		require.NoError(t, err)
		assert.NotNil(t, val)

		// Verify it's an AWS config
		config, ok := val.(aws.Config)
		assert.True(t, ok)
		assert.Equal(t, "us-east-1", config.Region)
	})

	t.Run("release resource", func(t *testing.T) {
		// Release the resource
		res.Release()

		// Try to get after release - should fail
		val, err := res.Get()
		assert.Error(t, err)
		assert.Equal(t, resource.ErrReleased, err)
		assert.Nil(t, val)

		// Release again - should be a no-op
		res.Release() // Make sure this doesn't panic
	})
}

// decodeConfig runs the config entry through the central decode pass so the test
// observes the same *_env resolution that the manager relies on.
func decodeConfig(ctx context.Context, t *testing.T, dtt payload.Transcoder, data map[string]any) *awsconfigapi.Config {
	t.Helper()
	entry := registry.Entry{
		ID:   registry.NewID("test", "awsconfig-decode"),
		Kind: awsconfigapi.Kind,
		Data: NewMockPayload(data),
	}
	cfg, err := entryutil.DecodeEntryConfig[awsconfigapi.Config](ctx, dtt, entry)
	require.NoError(t, err)
	return cfg
}

func TestCreateAWSConfig(t *testing.T) {
	manager, _, ctx := setupTestEnvironment(t)

	t.Run("with credentials", func(t *testing.T) {
		// The *_env directives resolve into the plain credential fields at decode.
		cfg := decodeConfig(ctx, t, manager.dtt, map[string]any{
			"region":                "us-east-1",
			"access_key_id_env":     "AWS_ACCESS_KEY_ID",
			"secret_access_key_env": "AWS_SECRET_ACCESS_KEY",
		})
		assert.Equal(t, "test-access-key", cfg.AccessKeyID)
		assert.Equal(t, "test-secret-key", cfg.SecretAccessKey)

		awsCfg, err := manager.createAWSConfig(ctx, cfg)
		require.NoError(t, err)
		assert.Equal(t, "us-east-1", awsCfg.Region)

		// Test credentials provider
		creds, err := awsCfg.Credentials.Retrieve(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "test-access-key", creds.AccessKeyID)
		assert.Equal(t, "test-secret-key", creds.SecretAccessKey)
	})

	t.Run("without credentials", func(t *testing.T) {
		cfg := decodeConfig(ctx, t, manager.dtt, map[string]any{
			"region": "us-west-2",
			// No credential directives specified
		})
		assert.Empty(t, cfg.AccessKeyID)
		assert.Empty(t, cfg.SecretAccessKey)

		awsCfg, err := manager.createAWSConfig(ctx, cfg)
		require.NoError(t, err)
		assert.Equal(t, "us-west-2", awsCfg.Region)
	})
}
