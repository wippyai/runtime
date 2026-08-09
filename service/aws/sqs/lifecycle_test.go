// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	sqsapi "github.com/wippyai/runtime/api/service/aws/sqs"
	"go.uber.org/zap"
)

type sqsBoundaryResource struct {
	value    any
	releases atomic.Int32
}

func (r *sqsBoundaryResource) Get() (any, error) { return r.value, nil }
func (r *sqsBoundaryResource) Release()          { r.releases.Add(1) }

type sqsBoundaryRegistry struct{ r resource.Resource[any] }

func (r sqsBoundaryRegistry) Acquire(context.Context, registry.ID, resource.AccessMode) (resource.Resource[any], error) {
	return r.r, nil
}
func (sqsBoundaryRegistry) List() ([]registry.ID, error) { return nil, nil }
func (sqsBoundaryRegistry) Exists(registry.ID) bool      { return true }

type sqsBoundaryTranscoder struct{}

func (sqsBoundaryTranscoder) Unmarshal(p payload.Payload, dst any) error {
	encoded, err := json.Marshal(p.Data())
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, dst)
}
func (sqsBoundaryTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

type recordingBus struct {
	events []event.Event
	mu     sync.Mutex
}

func (*recordingBus) Subscribe(context.Context, event.System, chan<- event.Event) (event.SubscriberID, error) {
	return "", errors.New("unused")
}
func (*recordingBus) SubscribeP(context.Context, event.System, event.Kind, chan<- event.Event) (event.SubscriberID, error) {
	return "", errors.New("unused")
}
func (*recordingBus) Unsubscribe(context.Context, event.SubscriberID) {}
func (b *recordingBus) Send(_ context.Context, e event.Event) {
	b.mu.Lock()
	b.events = append(b.events, e)
	b.mu.Unlock()
}
func (b *recordingBus) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.events)
}

func sqsBoundaryEntry(id registry.ID) registry.Entry {
	return registry.Entry{
		ID: id, Kind: sqsapi.Kind,
		Data: payload.New(map[string]any{"config": "aws/config", "endpoint": "http://localhost"}),
	}
}

func TestD04SQSManagerReleasesConfigOnSuccess(t *testing.T) {
	configResource := &sqsBoundaryResource{value: aws.Config{Region: "test"}}
	bus := &recordingBus{}
	manager := NewManager(bus, sqsBoundaryTranscoder{}, zap.NewNop())
	ctx := resource.WithRegistry(ctxapi.NewRootContext(), sqsBoundaryRegistry{r: configResource})
	id := registry.ParseID("test:sqs")

	require.NoError(t, manager.Add(ctx, sqsBoundaryEntry(id)))
	require.EqualValues(t, 1, configResource.releases.Load())
	manager.mu.RLock()
	driver := manager.drivers[id]
	manager.mu.RUnlock()
	require.NotNil(t, driver)
	require.Equal(t, 2, bus.count(), "installed driver must emit supervisor and queue registrations")
}

func TestD05SQSManagerReleasesConfigOnWrongType(t *testing.T) {
	configResource := &sqsBoundaryResource{value: "not an aws config"}
	bus := &recordingBus{}
	manager := NewManager(bus, sqsBoundaryTranscoder{}, zap.NewNop())
	ctx := resource.WithRegistry(ctxapi.NewRootContext(), sqsBoundaryRegistry{r: configResource})
	id := registry.ParseID("test:sqs-wrong-type")

	err := manager.Add(ctx, sqsBoundaryEntry(id))
	require.Error(t, err)
	var typed apierror.Error
	require.ErrorAs(t, err, &typed)
	require.Equal(t, apierror.Invalid, typed.Kind())
	require.EqualValues(t, 1, configResource.releases.Load())
	manager.mu.RLock()
	_, exists := manager.drivers[id]
	manager.mu.RUnlock()
	require.False(t, exists)
	require.Zero(t, bus.count())
}

func TestD07SQSStopIsIdempotent(t *testing.T) {
	cfg := &sqsapi.Config{}
	driver := NewDriver(registry.ParseID("test:sqs-stop"), cfg, aws.Config{Region: "test"}, sqsBoundaryTranscoder{}, zap.NewNop())
	status, err := driver.Start(context.Background())
	require.NoError(t, err)

	require.NoError(t, driver.Stop(context.Background()))
	_, open := <-status
	require.False(t, open)
	require.NotPanics(t, func() { require.NoError(t, driver.Stop(context.Background())) })
}
