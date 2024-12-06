package eventsbus

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/api/payload"

	"github.com/ponyruntime/pony/api"
	"github.com/stretchr/testify/require"
)

func TestEvenHandler(t *testing.T) {
	ctx := context.Background()
	eh, id := GlobalEventBus()
	defer eh.Unsubscribe(ctx, id)

	ch := make(chan api.Event, 100)
	err := eh.SubscribeP(ctx, id, api.ChangeGroup, api.EventConfigurationUpdated, ch)
	require.NoError(t, err)

	eh.Send(ctx, NewEvent(api.ChangeGroup, api.EventConfigurationUpdated, payload.NewString("new config")))

	evt := <-ch
	require.Equal(t, "new config", evt.Payload())
	require.Equal(t, api.ChangeGroup, evt.Component())
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

	eh.Send(context.Background(), NewEvent(api.SubSystemAll, api.EventConfigurationUpdated, payload.NewString("new config")))

	evt := <-ch
	require.Equal(t, "new config", evt.Payload())
	require.Equal(t, api.SubSystemAll, evt.Component())
	require.Equal(t, api.EventType("EventConfigurationUpdated"), evt.Kind())

	eh.Unsubscribe(ctx, id)
}
