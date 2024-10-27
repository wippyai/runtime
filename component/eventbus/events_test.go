package eventsbus

import (
	"context"
	"github.com/ponyruntime/pony/payload"
	"testing"

	"github.com/ponyruntime/pony/api"
	"github.com/stretchr/testify/require"
)

func TestEvenHandler(t *testing.T) {
	ctx := context.Background()
	eh, id := GlobalEventBus()
	defer eh.Unsubscribe(ctx, id)

	ch := make(chan api.Event, 100)
	err := eh.SubscribeP(ctx, id, api.Transaction, api.EventConfigurationUpdated, ch)
	require.NoError(t, err)

	eh.Send(ctx, NewEvent(api.EventConfigurationUpdated, api.Transaction, payload.NewString("new configuration")))

	evt := <-ch
	require.Equal(t, "new configuration", evt.Content())
	require.Equal(t, api.Transaction, evt.Target())
	require.Equal(t, api.EventType("EventConfigurationUpdated"), evt.Kind())

	eh.Unsubscribe(ctx, id)
}

func TestEvenHandler2(t *testing.T) {
	ctx := context.Background()
	eh, id := GlobalEventBus()
	defer eh.Unsubscribe(ctx, id)

	ch := make(chan api.Event, 100)
	err := eh.SubscribeP(ctx, id, api.SubSystemEndpoints, api.EventsAll, ch)
	require.NoError(t, err)

	eh.Send(context.Background(), NewEvent(api.EventConfigurationUpdated, api.SubSystemAll, payload.NewString("new configuration")))

	evt := <-ch
	require.Equal(t, "new configuration", evt.Content())
	require.Equal(t, api.SubSystemAll, evt.Target())
	require.Equal(t, api.EventType("EventConfigurationUpdated"), evt.Kind())

	eh.Unsubscribe(ctx, id)
}
