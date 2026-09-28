// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
)

var (
	wireAt     = time.Unix(1_700_000_000, 123_456_789).UTC()
	wireCaller = pid.PID{Node: "node-a", Host: "watchers", UniqID: "caller"}
	wireTarget = pid.PID{Node: "node-b", Host: "workers", UniqID: "target"}
)

// roundTripPayload sends one payload through the codec on TopicEvents and
// returns the decoded payload.
func roundTripPayload(t *testing.T, p payload.Payload) payload.Payload {
	t.Helper()
	c := NewMessageCodec(createRealTranscoder())
	data, err := c.Encode(relay.NewPackage(wireCaller, wireTarget, topology.TopicEvents, p))
	require.NoError(t, err)
	decoded, err := c.Decode(data)
	require.NoError(t, err)
	t.Cleanup(func() { relay.ReleasePackage(decoded) })
	require.Len(t, decoded.Messages, 1)
	require.Len(t, decoded.Messages[0].Payloads, 1)
	return decoded.Messages[0].Payloads[0]
}

func requireSamePID(t *testing.T, want, got pid.PID) {
	t.Helper()
	assert.Equal(t, want.String(), got.String())
}

func TestMessageCodec_TopologyRequestEventsKeepConcreteTypes(t *testing.T) {
	t.Run("MonitorRequestEvent", func(t *testing.T) {
		got := roundTripPayload(t, payload.New(&topology.MonitorRequestEvent{
			At: wireAt, Kind: topology.MonitorRequest, Caller: wireCaller, Target: wireTarget,
		}))
		assert.Equal(t, payload.Golang, got.Format())
		event, ok := got.Data().(*topology.MonitorRequestEvent)
		require.True(t, ok, "decoded %T", got.Data())
		assert.True(t, wireAt.Equal(event.At))
		assert.Equal(t, topology.MonitorRequest, event.Kind)
		requireSamePID(t, wireCaller, event.Caller)
		requireSamePID(t, wireTarget, event.Target)
	})

	t.Run("MonitorReleaseEvent", func(t *testing.T) {
		got := roundTripPayload(t, payload.New(&topology.MonitorReleaseEvent{
			At: wireAt, Kind: topology.MonitorRelease, Caller: wireCaller, Target: wireTarget,
		}))
		event, ok := got.Data().(*topology.MonitorReleaseEvent)
		require.True(t, ok, "decoded %T", got.Data())
		assert.True(t, wireAt.Equal(event.At))
		assert.Equal(t, topology.MonitorRelease, event.Kind)
		requireSamePID(t, wireCaller, event.Caller)
		requireSamePID(t, wireTarget, event.Target)
	})

	t.Run("LinkRequestEvent", func(t *testing.T) {
		got := roundTripPayload(t, payload.New(&topology.LinkRequestEvent{
			At: wireAt, Kind: topology.LinkRequest, From: wireCaller, To: wireTarget,
		}))
		event, ok := got.Data().(*topology.LinkRequestEvent)
		require.True(t, ok, "decoded %T", got.Data())
		assert.True(t, wireAt.Equal(event.At))
		assert.Equal(t, topology.LinkRequest, event.Kind)
		requireSamePID(t, wireCaller, event.From)
		requireSamePID(t, wireTarget, event.To)
	})

	t.Run("UnlinkRequestEvent", func(t *testing.T) {
		got := roundTripPayload(t, payload.New(&topology.UnlinkRequestEvent{
			At: wireAt, Kind: topology.UnlinkRequest, From: wireCaller, To: wireTarget,
		}))
		event, ok := got.Data().(*topology.UnlinkRequestEvent)
		require.True(t, ok, "decoded %T", got.Data())
		assert.True(t, wireAt.Equal(event.At))
		assert.Equal(t, topology.UnlinkRequest, event.Kind)
		requireSamePID(t, wireCaller, event.From)
		requireSamePID(t, wireTarget, event.To)
	})

	t.Run("CancelEvent", func(t *testing.T) {
		got := roundTripPayload(t, payload.New(&topology.CancelEvent{
			At: wireAt, Kind: topology.Cancel, From: wireCaller, Reason: "name revoked",
		}))
		event, ok := got.Data().(*topology.CancelEvent)
		require.True(t, ok, "decoded %T", got.Data())
		assert.True(t, wireAt.Equal(event.At))
		assert.Equal(t, topology.Cancel, event.Kind)
		requireSamePID(t, wireCaller, event.From)
		assert.Equal(t, "name revoked", event.Reason)
	})

	t.Run("OutdatedEvent", func(t *testing.T) {
		sources := []registry.ID{registry.NewID("app", "lib"), registry.NewID("app", "worker")}
		got := roundTripPayload(t, payload.New(&topology.OutdatedEvent{Sources: sources}))
		event, ok := got.Data().(*topology.OutdatedEvent)
		require.True(t, ok, "decoded %T", got.Data())
		require.Len(t, event.Sources, 2)
		assert.Equal(t, "app:lib", event.Sources[0].String())
		assert.Equal(t, "app:worker", event.Sources[1].String())
	})
}

func TestMessageCodec_ExitEventCarriesTypedResult(t *testing.T) {
	t.Run("api error keeps kind retryable and details", func(t *testing.T) {
		crash := apierror.New(apierror.Unavailable, "controller crashed").
			WithRetryable(apierror.True).
			WithDetails(attrs.Bag{"shard": "s-7"})
		got := roundTripPayload(t, payload.New(&topology.ExitEvent{
			At: wireAt, Kind: topology.Exit, From: wireTarget, Result: &runtime.Result{Error: crash},
		}))
		event, ok := got.Data().(*topology.ExitEvent)
		require.True(t, ok, "decoded %T", got.Data())
		assert.True(t, wireAt.Equal(event.At))
		assert.Equal(t, topology.Exit, event.Kind)
		requireSamePID(t, wireTarget, event.From)
		require.NotNil(t, event.Result)
		assert.Nil(t, event.Result.Value)
		require.Error(t, event.Result.Error)
		assert.Equal(t, "controller crashed", event.Result.Error.Error())

		var rich apierror.Rich
		require.True(t, errors.As(event.Result.Error, &rich))
		assert.Equal(t, apierror.Unavailable, rich.Kind())
		assert.Equal(t, apierror.True, rich.Retryable())
		assert.Equal(t, map[string]any{"shard": "s-7"}, rich.Details())
	})

	t.Run("plain error keeps its message", func(t *testing.T) {
		got := roundTripPayload(t, payload.New(&topology.ExitEvent{
			At: wireAt, Kind: topology.LinkDown, From: wireTarget,
			Result: &runtime.Result{Error: errors.New("node disconnected")},
		}))
		event, ok := got.Data().(*topology.ExitEvent)
		require.True(t, ok, "decoded %T", got.Data())
		assert.Equal(t, topology.LinkDown, event.Kind)
		require.NotNil(t, event.Result)
		assert.EqualError(t, event.Result.Error, "node disconnected")
	})

	t.Run("value keeps its payload format", func(t *testing.T) {
		got := roundTripPayload(t, payload.New(&topology.ExitEvent{
			At: wireAt, Kind: topology.Exit, From: wireTarget,
			Result: &runtime.Result{Value: payload.NewString("last checkpoint")},
		}))
		event, ok := got.Data().(*topology.ExitEvent)
		require.True(t, ok, "decoded %T", got.Data())
		require.NotNil(t, event.Result)
		assert.NoError(t, event.Result.Error)
		require.NotNil(t, event.Result.Value)
		assert.Equal(t, payload.String, event.Result.Value.Format())
		assert.Equal(t, "last checkpoint", event.Result.Value.Data())
	})

	t.Run("golang value keeps its data", func(t *testing.T) {
		got := roundTripPayload(t, payload.New(&topology.ExitEvent{
			At: wireAt, Kind: topology.Exit, From: wireTarget,
			Result: &runtime.Result{Value: payload.New(map[string]any{"count": "3"})},
		}))
		event, ok := got.Data().(*topology.ExitEvent)
		require.True(t, ok, "decoded %T", got.Data())
		require.NotNil(t, event.Result.Value)
		assert.Equal(t, payload.Golang, event.Result.Value.Format())
		assert.Equal(t, map[string]any{"count": "3"}, event.Result.Value.Data())
	})

	t.Run("nil result stays nil", func(t *testing.T) {
		got := roundTripPayload(t, payload.New(&topology.ExitEvent{
			At: wireAt, Kind: topology.Exit, From: wireTarget,
		}))
		event, ok := got.Data().(*topology.ExitEvent)
		require.True(t, ok, "decoded %T", got.Data())
		assert.Nil(t, event.Result)
	})
}

func TestMessageCodec_GoErrorPayloadRoundTrip(t *testing.T) {
	t.Run("api error", func(t *testing.T) {
		cause := errors.New("connection refused")
		sent := apierror.New(apierror.Timeout, "call timed out").
			WithRetryable(apierror.False).
			WithDetails(attrs.Bag{"method": "fetch"}).
			WithCause(cause)
		got := roundTripPayload(t, payload.NewError(sent))
		assert.Equal(t, payload.GoError, got.Format())
		err, ok := got.Data().(error)
		require.True(t, ok, "decoded %T", got.Data())
		assert.Equal(t, sent.Error(), err.Error())

		var rich apierror.Rich
		require.True(t, errors.As(err, &rich))
		assert.Equal(t, apierror.Timeout, rich.Kind())
		assert.Equal(t, apierror.False, rich.Retryable())
		assert.Equal(t, map[string]any{"method": "fetch"}, rich.Details())
		require.NotNil(t, errors.Unwrap(err))
		assert.Equal(t, "connection refused", errors.Unwrap(err).Error())
	})

	t.Run("plain error", func(t *testing.T) {
		got := roundTripPayload(t, payload.NewError(errors.New("boom")))
		err, ok := got.Data().(error)
		require.True(t, ok, "decoded %T", got.Data())
		assert.EqualError(t, err, "boom")
	})
}
