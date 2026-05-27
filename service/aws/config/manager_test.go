// SPDX-License-Identifier: MPL-2.0

package config

import (
	"context"
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
	"github.com/wippyai/runtime/system/eventbus"
	systemresource "github.com/wippyai/runtime/system/resource"
	"go.uber.org/zap"
)

// MockPayload implements payload.Payload for testing
type MockPayload struct {
	data   any
	format payload.Format
}

func (p *MockPayload) Data() any {
	return p.data
}

func (p *MockPayload) Format() payload.Format {
	return p.format
}

func (p *MockPayload) Transcode(format payload.Format) (payload.Payload, error) {
	return &MockPayload{data: p.data, format: format}, nil
}

// Function to create mock payloads
func NewMockPayload(data any) payload.Payload {
	return &MockPayload{data: data, format: payload.Golang}
}

// MockTranscoder implements payload.Transcoder for testing
type MockTranscoder struct {
	marshalError       error
	unmarshalError     error
	region             string
	regionEnv          string
	accessKeyIDEnv     string
	secretAccessKeyEnv string
	mockData           []byte
}

func NewMockTranscoder() *MockTranscoder {
	return &MockTranscoder{
		mockData:           []byte(`{"region":"us-east-1","access_key_id_env":"AWS_ACCESS_KEY_ID","secret_access_key_env":"AWS_SECRET_ACCESS_KEY"}`),
		region:             "us-east-1",
		accessKeyIDEnv:     "AWS_ACCESS_KEY_ID",
		secretAccessKeyEnv: "AWS_SECRET_ACCESS_KEY",
	}
}

func (m *MockTranscoder) Marshal(_ any) ([]byte, error) {
	if m.marshalError != nil {
		return nil, m.marshalError
	}
	return m.mockData, nil
}

func (m *MockTranscoder) Unmarshal(p payload.Payload, v any) error {
	if m.unmarshalError != nil {
		return m.unmarshalError
	}

	// Use the actual data from the payload
	if cfg, ok := v.(*awsconfigapi.Config); ok {
		if payloadData, ok := p.Data().(*awsconfigapi.Config); ok {
			// Copy the values from the payload
			cfg.Region = payloadData.Region
			cfg.RegionEnv = payloadData.RegionEnv
			cfg.AccessKeyIDEnv = payloadData.AccessKeyIDEnv
			cfg.SecretAccessKeyEnv = payloadData.SecretAccessKeyEnv
		} else {
			// Fallback to predefined values if payload data is not the expected type
			cfg.Region = m.region
			cfg.RegionEnv = m.regionEnv
			cfg.AccessKeyIDEnv = m.accessKeyIDEnv
			cfg.SecretAccessKeyEnv = m.secretAccessKeyEnv
		}
	}

	return nil
}

func (m *MockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
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
	// For testing purposes, we return the variables map
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

// setupTestEnvironment creates a test environment with mocked dependencies
func setupTestEnvironment(t *testing.T) (*Manager, event.Bus, context.Context) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	// Set up the mock transcoder
	transcoder := NewMockTranscoder()

	// Create mock registry and populate it with test values
	envRegistry := NewMockRegistry()

	ctx := ctxapi.NewRootContext()

	require.NoError(t, envRegistry.Set(ctx, "AWS_ACCESS_KEY_ID", "test-access-key"))
	require.NoError(t, envRegistry.Set(ctx, "AWS_SECRET_ACCESS_KEY", "test-secret-key"))
	require.NoError(t, envRegistry.Set(ctx, "AWS_REGION", "eu-west-1"))

	// Create manager
	manager := NewManager(bus, transcoder, logger, envRegistry)

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
//

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
			Data: NewMockPayload(&awsconfigapi.Config{
				Region:             "us-east-1",
				AccessKeyIDEnv:     "AWS_ACCESS_KEY_ID",
				SecretAccessKeyEnv: "AWS_SECRET_ACCESS_KEY",
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
	})

	t.Run("successful config addition with region env", func(t *testing.T) {
		envID := registry.NewID("test", "awsconfig-region-env")
		entry := registry.Entry{
			ID:   envID,
			Kind: awsconfigapi.Kind,
			Data: NewMockPayload(&awsconfigapi.Config{
				RegionEnv:          "AWS_REGION",
				AccessKeyIDEnv:     "AWS_ACCESS_KEY_ID",
				SecretAccessKeyEnv: "AWS_SECRET_ACCESS_KEY",
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
			Data: NewMockPayload(&awsconfigapi.Config{
				Region: "us-east-1",
			}),
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
			Data: NewMockPayload("invalid json"),
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
			Data: NewMockPayload(&awsconfigapi.Config{
				Region: "us-east-1",
			}),
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
		Data: NewMockPayload(&awsconfigapi.Config{
			Region:             "us-east-1",
			AccessKeyIDEnv:     "AWS_ACCESS_KEY_ID",
			SecretAccessKeyEnv: "AWS_SECRET_ACCESS_KEY",
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
			Data: NewMockPayload(&awsconfigapi.Config{
				Region:             "us-west-2",
				AccessKeyIDEnv:     "AWS_ACCESS_KEY_ID",
				SecretAccessKeyEnv: "AWS_SECRET_ACCESS_KEY",
			}),
		}

		// Configure transcoder to return updated values
		customTranscoder := NewMockTranscoder()
		customTranscoder.region = "us-west-2"

		// Replace the manager's transcoder
		originalTranscoder := manager.dtt
		manager.dtt = customTranscoder

		// Update the config
		err := manager.Update(ctx, updateEntry)
		require.NoError(t, err)

		// Reset transcoder
		manager.dtt = originalTranscoder

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
			Data: NewMockPayload(&awsconfigapi.Config{
				Region: "us-east-1",
			}),
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
			Data: NewMockPayload(&awsconfigapi.Config{}),
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
			Data: NewMockPayload("invalid json"),
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
		Data: NewMockPayload(&awsconfigapi.Config{
			Region:             "us-east-1",
			AccessKeyIDEnv:     "AWS_ACCESS_KEY_ID",
			SecretAccessKeyEnv: "AWS_SECRET_ACCESS_KEY",
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
			Data: NewMockPayload(&awsconfigapi.Config{}),
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
		Data: NewMockPayload(&awsconfigapi.Config{
			Region:             "us-east-1",
			AccessKeyIDEnv:     "AWS_ACCESS_KEY_ID",
			SecretAccessKeyEnv: "AWS_SECRET_ACCESS_KEY",
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
		Data: NewMockPayload(&awsconfigapi.Config{
			Region:             "us-east-1",
			AccessKeyIDEnv:     "AWS_ACCESS_KEY_ID",
			SecretAccessKeyEnv: "AWS_SECRET_ACCESS_KEY",
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

func TestCreateAWSConfig(t *testing.T) {
	manager, _, ctx := setupTestEnvironment(t)

	t.Run("with credentials", func(t *testing.T) {
		cfg := &awsconfigapi.Config{
			Region:             "us-east-1",
			AccessKeyIDEnv:     "AWS_ACCESS_KEY_ID",
			SecretAccessKeyEnv: "AWS_SECRET_ACCESS_KEY",
		}

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
		cfg := &awsconfigapi.Config{
			Region: "us-west-2",
			// No credential env vars specified
		}

		awsCfg, err := manager.createAWSConfig(ctx, cfg)
		require.NoError(t, err)
		assert.Equal(t, "us-west-2", awsCfg.Region)
	})
}
