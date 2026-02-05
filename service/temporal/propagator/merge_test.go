package propagator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	secapi "github.com/wippyai/runtime/api/security"
)

func TestMergeActivityContext_NoValuesNoSecurity(t *testing.T) {
	appCtx := context.Background()
	activityCtx := context.Background()

	merged, release, err := MergeActivityContext(appCtx, activityCtx)
	require.NoError(t, err)
	assert.Equal(t, appCtx, merged)
	release()
}

func TestMergeActivityContext_WithContextValues(t *testing.T) {
	appCtx := context.Background()
	app := ctxapi.NewAppContext()
	appCtx = ctxapi.WithAppContext(appCtx, app)

	activityCtx := WithValues(context.Background(), map[string]any{
		"tenant": "acme",
		"user":   "alice",
	})

	merged, release, err := MergeActivityContext(appCtx, activityCtx)
	require.NoError(t, err)
	defer release()

	assert.NotEqual(t, appCtx, merged)

	values := ctxapi.GetValues(merged)
	require.NotNil(t, values)
	val, ok := values.Get("tenant")
	require.True(t, ok)
	assert.Equal(t, "acme", val)
	val, ok = values.Get("user")
	require.True(t, ok)
	assert.Equal(t, "alice", val)
}

func TestMergeActivityContext_WithSecurityPayload(t *testing.T) {
	appCtx := context.Background()
	app := ctxapi.NewAppContext()
	appCtx = ctxapi.WithAppContext(appCtx, app)

	activityCtx := WithSecurityCtx(context.Background(), &SecurityPayload{
		Actor: &ActorPayload{
			ID:   "user-123",
			Meta: map[string]any{"role": "admin"},
		},
	})

	merged, release, err := MergeActivityContext(appCtx, activityCtx)
	require.NoError(t, err)
	defer release()

	assert.NotEqual(t, appCtx, merged)

	actor, ok := secapi.GetActor(merged)
	require.True(t, ok)
	assert.Equal(t, "user-123", actor.ID)
}

func TestMergeActivityContext_WithValuesAndSecurity(t *testing.T) {
	appCtx := context.Background()
	app := ctxapi.NewAppContext()
	appCtx = ctxapi.WithAppContext(appCtx, app)

	activityCtx := context.Background()
	activityCtx = WithValues(activityCtx, map[string]any{"key": "value"})
	activityCtx = WithSecurityCtx(activityCtx, &SecurityPayload{
		Actor: &ActorPayload{ID: "user-456"},
	})

	merged, release, err := MergeActivityContext(appCtx, activityCtx)
	require.NoError(t, err)
	defer release()

	values := ctxapi.GetValues(merged)
	require.NotNil(t, values)
	val, ok := values.Get("key")
	require.True(t, ok)
	assert.Equal(t, "value", val)

	actor, ok := secapi.GetActor(merged)
	require.True(t, ok)
	assert.Equal(t, "user-456", actor.ID)
}

func TestMergeActivityContext_OnlyContextValues(t *testing.T) {
	appCtx := context.Background()
	app := ctxapi.NewAppContext()
	appCtx = ctxapi.WithAppContext(appCtx, app)

	activityCtx := WithValues(context.Background(), map[string]any{"only": "values"})

	merged, release, err := MergeActivityContext(appCtx, activityCtx)
	require.NoError(t, err)
	defer release()

	values := ctxapi.GetValues(merged)
	require.NotNil(t, values)
	val, ok := values.Get("only")
	require.True(t, ok)
	assert.Equal(t, "values", val)

	// No actor should be set
	_, hasActor := secapi.GetActor(merged)
	assert.False(t, hasActor)
}

func TestMergeActivityContext_OnlySecurityPayload(t *testing.T) {
	appCtx := context.Background()
	app := ctxapi.NewAppContext()
	appCtx = ctxapi.WithAppContext(appCtx, app)

	activityCtx := WithSecurityCtx(context.Background(), &SecurityPayload{
		Actor: &ActorPayload{ID: "only-actor"},
	})

	merged, release, err := MergeActivityContext(appCtx, activityCtx)
	require.NoError(t, err)
	defer release()

	actor, ok := secapi.GetActor(merged)
	require.True(t, ok)
	assert.Equal(t, "only-actor", actor.ID)
}
