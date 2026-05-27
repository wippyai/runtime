// SPDX-License-Identifier: MPL-2.0

package s3

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cloudstorage"
	ctxapi "github.com/wippyai/runtime/api/context"
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	services3 "github.com/wippyai/runtime/api/service/aws/s3"
	"github.com/wippyai/runtime/system/eventbus"
	systemresource "github.com/wippyai/runtime/system/resource"
	"go.uber.org/zap"
)

// MockResource implements resource.Resource[any] for testing
type MockResource struct {
	value any
	err   error
}

func (r *MockResource) Get() (any, error) {
	return r.value, r.err
}

func (r *MockResource) Release() {
	// No-op for testing
}

// MockResourceProvider implements resource.Provider for testing
type MockResourceProvider struct {
	resources map[registry.ID]*MockResource
}

func NewMockResourceProvider() *MockResourceProvider {
	return &MockResourceProvider{
		resources: make(map[registry.ID]*MockResource),
	}
}

func (p *MockResourceProvider) AddResource(id registry.ID, value any, err error) {
	p.resources[id] = &MockResource{value: value, err: err}
}

func (p *MockResourceProvider) Acquire(_ context.Context, id registry.ID, _ resource.AccessMode) (resource.Resource[any], error) {
	res, ok := p.resources[id]
	if !ok {
		return nil, errors.New("resource not found")
	}
	return res, nil
}

// MockResourceRegistry implements resource.Registry for testing
type MockResourceRegistry struct {
	providers map[registry.ID]resource.Provider
}

func NewMockResourceRegistry() *MockResourceRegistry {
	return &MockResourceRegistry{
		providers: make(map[registry.ID]resource.Provider),
	}
}

func (r *MockResourceRegistry) RegisterProvider(id registry.ID, provider resource.Provider) {
	r.providers[id] = provider
}

func (r *MockResourceRegistry) Acquire(ctx context.Context, id registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	provider, ok := r.providers[id]
	if !ok {
		return nil, errors.New("provider not found")
	}
	return provider.Acquire(ctx, id, mode)
}

func (r *MockResourceRegistry) List() ([]registry.ID, error) {
	ids := make([]registry.ID, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	return ids, nil
}

func (r *MockResourceRegistry) Exists(id registry.ID) bool {
	_, ok := r.providers[id]
	return ok
}

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
	marshalError   error
	unmarshalError error
	bucket         string
	bucketEnv      string
	awsConfig      string
	endpoint       string
	endpointEnv    string
	mockData       []byte
}

func NewMockTranscoder() *MockTranscoder {
	return &MockTranscoder{
		mockData:  []byte(`{"bucket":"test-bucket","config":"aws/config","endpoint":"http://localhost:4566"}`),
		bucket:    "test-bucket",
		awsConfig: "aws/config",
		endpoint:  "http://localhost:4566",
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

	// For simplicity, mock implementation that sets predefined values
	if cfg, ok := v.(*services3.Config); ok {
		if payloadData, ok := p.Data().(*services3.Config); ok {
			cfg.Bucket = payloadData.Bucket
			cfg.BucketEnv = payloadData.BucketEnv
			cfg.AWSConfig = payloadData.AWSConfig
			cfg.Endpoint = payloadData.Endpoint
			cfg.EndpointEnv = payloadData.EndpointEnv
		} else {
			cfg.Bucket = m.bucket
			cfg.BucketEnv = m.bucketEnv
			cfg.AWSConfig = m.awsConfig
			cfg.Endpoint = m.endpoint
			cfg.EndpointEnv = m.endpointEnv
		}
	}

	return nil
}

func (m *MockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

type MockEnvRegistry struct {
	variables map[string]string
}

func NewMockEnvRegistry() *MockEnvRegistry {
	return &MockEnvRegistry{variables: make(map[string]string)}
}

func (m *MockEnvRegistry) Get(_ context.Context, name string) (string, error) {
	if value, ok := m.variables[name]; ok {
		return value, nil
	}
	return "", envapi.ErrVariableNotFound
}

func (m *MockEnvRegistry) Lookup(_ context.Context, name string) (string, bool, error) {
	if value, ok := m.variables[name]; ok {
		return value, true, nil
	}
	return "", false, nil
}

func (m *MockEnvRegistry) Set(_ context.Context, name string, value string) error {
	m.variables[name] = value
	return nil
}

func (m *MockEnvRegistry) All(_ context.Context) (map[string]string, error) {
	return m.variables, nil
}

func (m *MockEnvRegistry) GetStorage(_ context.Context, _ registry.ID) (envapi.Storage, error) {
	return nil, envapi.ErrStorageNotFound
}

func (m *MockEnvRegistry) RegisterStorage(_ registry.ID, _ envapi.Storage) {}

// setupTestEnvironment creates a test environment with mocked dependencies
//

func setupTestEnvironment() (*Manager, event.Bus, *MockResourceRegistry, context.Context) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	// Set up the mock transcoder
	transcoder := NewMockTranscoder()
	envRegistry := NewMockEnvRegistry()
	_ = envRegistry.Set(context.Background(), "S3_BUCKET", "env-bucket")
	_ = envRegistry.Set(context.Background(), "S3_ENDPOINT", "http://env-minio:9000")

	// Create manager
	manager := NewManager(bus, transcoder, logger, envRegistry)

	// Set up resource registry with AWS config provider
	resourceRegistry := NewMockResourceRegistry()

	// Create a mock resource provider for AWS config
	awsConfigProvider := NewMockResourceProvider()

	// Add a mock AWS config resource
	awsConfig := aws.Config{
		Region: "us-east-1",
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     "test",
				SecretAccessKey: "test",
			}, nil
		}),
	}

	// Register the AWS config resource
	configID := registry.ParseID("aws/config")
	awsConfigProvider.AddResource(configID, awsConfig, nil)
	resourceRegistry.RegisterProvider(configID, awsConfigProvider)

	// Create context with resource registry
	ctx := resource.WithRegistry(ctxapi.NewRootContext(), resourceRegistry)

	return manager, bus, resourceRegistry, ctx
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
	manager, bus, _, ctx := setupTestEnvironment()

	// Set up event listener for resource events
	resourceEvents, cleanup, err := setupResourceEventsListener(ctx, bus)
	require.NoError(t, err)
	defer cleanup()

	testID := registry.NewID("test", "s3storage")

	t.Run("successful storage addition", func(t *testing.T) {
		entry := registry.Entry{
			ID:   testID,
			Kind: services3.Kind,
			Data: NewMockPayload(&services3.Config{
				Bucket:    "test-bucket",
				AWSConfig: "aws/config",
				Endpoint:  "http://localhost:4566",
			}),
		}

		err := manager.Add(ctx, entry)
		require.NoError(t, err)

		// Verify storage was added to the manager's map
		manager.mu.RLock()
		storage, exists := manager.storages[testID]
		manager.mu.RUnlock()

		assert.True(t, exists)
		assert.NotNil(t, storage)

		// Verify resource registration event was sent
		evt := waitForResourceEvent(t, resourceEvents, resource.Register, time.Second)
		assert.Equal(t, testID.String(), evt.Path)

		// Verify event data
		resourceEntry, ok := evt.Data.(resource.Entry)
		assert.True(t, ok)
		assert.Equal(t, manager, resourceEntry.Provider)

		// Verify metadata
		meta := resourceEntry.Meta
		assert.Equal(t, "test-bucket", meta["bucket"])
	})

	t.Run("successful storage addition from env fields", func(t *testing.T) {
		envID := registry.NewID("test", "s3storage-env")
		entry := registry.Entry{
			ID:   envID,
			Kind: services3.Kind,
			Data: NewMockPayload(&services3.Config{
				BucketEnv:   "S3_BUCKET",
				AWSConfig:   "aws/config",
				EndpointEnv: "S3_ENDPOINT",
			}),
		}

		err := manager.Add(ctx, entry)
		require.NoError(t, err)

		evt := waitForResourceEvent(t, resourceEvents, resource.Register, time.Second)
		assert.Equal(t, envID.String(), evt.Path)

		resourceEntry, ok := evt.Data.(resource.Entry)
		require.True(t, ok)
		assert.Equal(t, "env-bucket", resourceEntry.Meta["bucket"])
		assert.Equal(t, "http://env-minio:9000", resourceEntry.Meta["endpoint"])
	})

	t.Run("wrong entry kind", func(t *testing.T) {
		entry := registry.Entry{
			Kind: "invalid.kind",
			Data: NewMockPayload(&services3.Config{
				Bucket:    "test-bucket",
				AWSConfig: "aws/config",
				Endpoint:  "http://localhost:4566",
			}),
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported entry kind")
	})

	t.Run("unmarshal error", func(t *testing.T) {
		// Configure transcoder to return error
		manager.dtt = &MockTranscoder{unmarshalError: errors.New("unmarshal error")}

		entry := registry.Entry{
			Kind: services3.Kind,
			Data: NewMockPayload("invalid json"),
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "add entry")

		// Reset transcoder for other tests
		manager.dtt = NewMockTranscoder()
	})

	t.Run("duplicate storage", func(t *testing.T) {
		entry := registry.Entry{
			ID:   testID, // Same ID as in successful test
			Kind: services3.Kind,
			Data: NewMockPayload(&services3.Config{
				Bucket:    "test-bucket",
				AWSConfig: "aws/config",
				Endpoint:  "http://localhost:4566",
			}),
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("aws config resource not found", func(t *testing.T) {
		entry := registry.Entry{
			Kind: services3.Kind,
			Data: NewMockPayload(&services3.Config{}),
		}

		// Create a custom transcoder for this test
		customTranscoder := NewMockTranscoder()
		customTranscoder.awsConfig = "missing/config" // Non-existent config

		// Replace the manager's transcoder
		originalTranscoder := manager.dtt
		manager.dtt = customTranscoder

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "add entry")

		// Reset transcoder
		manager.dtt = originalTranscoder
	})
}

func TestManager_Update(t *testing.T) {
	manager, bus, _, ctx := setupTestEnvironment()

	// Set up event listener for resource events
	resourceEvents, cleanup, err := setupResourceEventsListener(ctx, bus)
	require.NoError(t, err)
	defer cleanup()

	testID := registry.NewID("test", "s3storage")

	// First add a storage
	addEntry := registry.Entry{
		ID:   testID,
		Kind: services3.Kind,
		Data: NewMockPayload(&services3.Config{
			Bucket:    "test-bucket",
			AWSConfig: "aws/config",
			Endpoint:  "http://localhost:4566",
		}),
	}

	err = manager.Add(ctx, addEntry)
	require.NoError(t, err)

	// Drain the add event
	waitForResourceEvent(t, resourceEvents, resource.Register, time.Second)

	t.Run("successful update", func(t *testing.T) {
		// Create update entry with the same ID
		updateEntry := registry.Entry{
			ID:   testID,
			Kind: services3.Kind,
			Data: NewMockPayload(&services3.Config{
				Bucket:    "updated-bucket",
				AWSConfig: "aws/config",
				Endpoint:  "http://localhost:9000", // Changed endpoint
			}),
		}

		// Configure transcoder to return updated values
		customTranscoder := NewMockTranscoder()
		customTranscoder.bucket = "updated-bucket"
		customTranscoder.endpoint = "http://localhost:9000"

		// Replace the manager's transcoder
		originalTranscoder := manager.dtt
		manager.dtt = customTranscoder

		// Update the storage
		err := manager.Update(ctx, updateEntry)
		require.NoError(t, err)

		// Reset transcoder
		manager.dtt = originalTranscoder

		// Verify resource update event was sent
		evt := waitForResourceEvent(t, resourceEvents, resource.Update, time.Second)
		assert.Equal(t, testID.String(), evt.Path)

		// Verify event data
		resourceEntry, ok := evt.Data.(resource.Entry)
		assert.True(t, ok)

		// Verify updated metadata
		meta := resourceEntry.Meta
		assert.Equal(t, "updated-bucket", meta["bucket"])
	})

	t.Run("storage not found", func(t *testing.T) {
		nonExistentID := registry.NewID("test", "nonexistent")
		entry := registry.Entry{
			ID:   nonExistentID,
			Kind: services3.Kind,
			Data: NewMockPayload(&services3.Config{
				Bucket:    "test-bucket",
				AWSConfig: "aws/config",
				Endpoint:  "http://localhost:4566",
			}),
		}

		err := manager.Update(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("wrong entry kind", func(t *testing.T) {
		entry := registry.Entry{
			ID:   testID,
			Kind: "invalid.kind",
			Data: NewMockPayload(&services3.Config{}),
		}

		err := manager.Update(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported entry kind")
	})

	t.Run("unmarshal error", func(t *testing.T) {
		// Configure transcoder to return error
		manager.dtt = &MockTranscoder{unmarshalError: errors.New("unmarshal error")}

		entry := registry.Entry{
			ID:   testID,
			Kind: services3.Kind,
			Data: NewMockPayload("invalid json"),
		}

		err := manager.Update(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "update entry")

		// Reset transcoder for other tests
		manager.dtt = NewMockTranscoder()
	})
}

func TestManager_Delete(t *testing.T) {
	manager, bus, _, ctx := setupTestEnvironment()

	// Set up event listener for resource events
	resourceEvents, cleanup, err := setupResourceEventsListener(ctx, bus)
	require.NoError(t, err)
	defer cleanup()

	testID := registry.NewID("test", "s3storage")

	// First add a storage
	addEntry := registry.Entry{
		ID:   testID,
		Kind: services3.Kind,
		Data: NewMockPayload(&services3.Config{
			Bucket:    "test-bucket",
			AWSConfig: "aws/config",
			Endpoint:  "http://localhost:4566",
		}),
	}

	err = manager.Add(ctx, addEntry)
	require.NoError(t, err)

	// Drain the add event
	waitForResourceEvent(t, resourceEvents, resource.Register, time.Second)

	t.Run("successful deletion", func(t *testing.T) {
		// Delete the storage
		err := manager.Delete(ctx, addEntry)
		require.NoError(t, err)

		// Verify storage was removed from the manager's map
		manager.mu.RLock()
		_, exists := manager.storages[testID]
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

	t.Run("storage not found", func(t *testing.T) {
		// Try to delete again (should fail as already deleted)
		err := manager.Delete(ctx, addEntry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("wrong entry kind", func(t *testing.T) {
		entry := registry.Entry{
			ID:   testID,
			Kind: "invalid.kind",
			Data: NewMockPayload(&services3.Config{}),
		}

		err := manager.Delete(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported entry kind")
	})
}

func TestManager_Acquire(t *testing.T) {
	manager, _, _, ctx := setupTestEnvironment()

	testID := registry.NewID("test", "s3storage")

	// Add a storage first
	addEntry := registry.Entry{
		ID:   testID,
		Kind: services3.Kind,
		Data: NewMockPayload(&services3.Config{
			Bucket:    "test-bucket",
			AWSConfig: "aws/config",
			Endpoint:  "http://localhost:4566",
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

		// Verify the resource is a cloud storage
		storage, ok := val.(cloudstorage.Storage)
		assert.True(t, ok)
		assert.NotNil(t, storage)
	})

	t.Run("resource not found", func(t *testing.T) {
		nonExistentID := registry.NewID("test", "nonexistent")

		// Try to acquire a non-existent resource
		res, err := manager.Acquire(ctx, nonExistentID, resource.ModeNormal)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
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

func TestS3Resource(t *testing.T) {
	manager, _, _, ctx := setupTestEnvironment()

	testID := registry.NewID("test", "s3storage")

	// Add a storage first
	addEntry := registry.Entry{
		ID:   testID,
		Kind: services3.Kind,
		Data: NewMockPayload(&services3.Config{
			Bucket:    "test-bucket",
			AWSConfig: "aws/config",
			Endpoint:  "http://localhost:4566",
		}),
	}

	err := manager.Add(ctx, addEntry)
	require.NoError(t, err)

	// Acquire the resource
	res, err := manager.Acquire(ctx, testID, resource.ModeNormal)
	require.NoError(t, err)
	require.NotNil(t, res)

	t.Run("get storage", func(t *testing.T) {
		// Get the resource value
		val, err := res.Get()
		require.NoError(t, err)
		assert.NotNil(t, val)

		// Verify it's a storage
		_, ok := val.(cloudstorage.Storage)
		assert.True(t, ok)
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
