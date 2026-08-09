// SPDX-License-Identifier: MPL-2.0

package s3

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cloudstorage"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	services3 "github.com/wippyai/runtime/api/service/aws/s3"
)

type countingConfigResource struct {
	value    any
	getErr   error
	releases atomic.Int32
}

func (r *countingConfigResource) Get() (any, error) { return r.value, r.getErr }
func (r *countingConfigResource) Release()          { r.releases.Add(1) }

func TestD02S3ManagerReleasesConfigOnSuccess(t *testing.T) {
	manager, _, resources, ctx := setupTestEnvironment()
	cfgResource := &countingConfigResource{value: aws.Config{Region: "test"}}
	resources.RegisterProvider(registry.ParseID("aws/config"), resourceProviderFunc(func() resource.Resource[any] {
		return cfgResource
	}))
	id := registry.ParseID("test:storage")

	err := manager.Add(ctx, registry.Entry{
		ID: id, Kind: services3.Kind,
		Data: NewMockPayload(map[string]any{"bucket": "bucket", "config": "aws/config"}),
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, cfgResource.releases.Load())

	acquired, err := manager.Acquire(ctx, id, resource.ModeNormal)
	require.NoError(t, err)
	t.Cleanup(acquired.Release)
	got, err := acquired.Get()
	require.NoError(t, err)
	_, ok := got.(cloudstorage.Storage)
	require.True(t, ok, "installed storage must remain usable after config release")
}

func TestD03S3ManagerReleasesConfigOnGetFailure(t *testing.T) {
	manager, _, resources, ctx := setupTestEnvironment()
	getErr := errors.New("config get failed")
	cfgResource := &countingConfigResource{getErr: getErr}
	resources.RegisterProvider(registry.ParseID("aws/config"), resourceProviderFunc(func() resource.Resource[any] {
		return cfgResource
	}))
	id := registry.ParseID("test:storage-get-failure")

	err := manager.Add(ctx, registry.Entry{
		ID: id, Kind: services3.Kind,
		Data: NewMockPayload(map[string]any{"bucket": "bucket", "config": "aws/config"}),
	})
	require.ErrorIs(t, err, getErr)
	require.EqualValues(t, 1, cfgResource.releases.Load())
	manager.mu.RLock()
	_, exists := manager.storages[id]
	manager.mu.RUnlock()
	require.False(t, exists)
}

type resourceProviderFunc func() resource.Resource[any]

func (f resourceProviderFunc) Acquire(_ context.Context, _ registry.ID, _ resource.AccessMode) (resource.Resource[any], error) {
	return f(), nil
}
