package eventsbus

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/api"
	"github.com/stretchr/testify/require"
)

func TestEvenHandler(t *testing.T) {
	ctx := context.Background()
	eh, id := GlobalEventBus()
	defer eh.Unsubscribe(ctx, id)

	ch := make(chan api.Event, 100)
	err := eh.SubscribeP(ctx, id, api.SubSystemConfiguration, api.EventConfigurationUpdated, ch)
	require.NoError(t, err)

	eh.Send(ctx, NewEvent(api.EventConfigurationUpdated, api.SubSystemConfiguration, "new configuration"))

	evt := <-ch
	require.Equal(t, "new configuration", evt.Content())
	require.Equal(t, api.SubSystemConfiguration, evt.SubSystem())
	require.Equal(t, api.EventType("EventConfigurationUpdated"), evt.Type())

	eh.Unsubscribe(ctx, id)
}

func TestEvenHandler2(t *testing.T) {
	ctx := context.Background()
	eh, id := GlobalEventBus()
	defer eh.Unsubscribe(ctx, id)

	ch := make(chan api.Event, 100)
	err := eh.SubscribeP(ctx, id, api.SubSystemEndpoints, api.EventsAll, ch)
	require.NoError(t, err)

	eh.Send(context.Background(), NewEvent(api.EventConfigurationUpdated, api.SubSystemAll, "new configuration"))

	evt := <-ch
	require.Equal(t, "new configuration", evt.Content())
	require.Equal(t, api.SubSystemAll, evt.SubSystem())
	require.Equal(t, api.EventType("EventConfigurationUpdated"), evt.Type())

	eh.Unsubscribe(ctx, id)
}
