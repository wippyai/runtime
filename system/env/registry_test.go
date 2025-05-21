package env

import (
	"context"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	serviceenv "github.com/ponyruntime/pony/service/env"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type testCase struct {
	name      string
	accessBy  string // "id" or "env_name"
	value     string
	namespace string
}

func TestVariableAccess(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	//nolint:errcheck // ok for tests
	defer logger.Sync()

	ctx := context.Background()
	bus := eventbus.NewBus()
	defer bus.Stop()

	reg := NewRegistry(bus, logger)
	err = reg.Start(ctx)
	require.NoError(t, err)
	//nolint:errcheck // ok for tests
	defer reg.Stop()

	// Create a memory storage
	memStorage := serviceenv.NewMemoryStorage(map[string]string{
		"TEST_VAR": "initial_value",
	}, logger)

	// Register storage
	storageEvt := event.Event{
		System: env.System,
		Kind:   env.StorageRegister,
		Path:   "test:mock-storage",
		Data:   memStorage,
	}
	bus.Send(ctx, storageEvt)
	time.Sleep(100 * time.Millisecond)

	// Register a variable
	variable := env.Variable{
		Name:         "test_var",
		EnvName:      "TEST_VAR",
		StorageID:    "test:mock-storage",
		DefaultValue: "default_value",
		ReadOnly:     false,
	}
	varEvt := event.Event{
		System: env.System,
		Kind:   env.VariableRegister,
		Path:   "test:test_var",
		Data:   variable,
	}
	bus.Send(ctx, varEvt)
	time.Sleep(100 * time.Millisecond)

	testCases := []testCase{
		{
			name:      "Access by ID",
			accessBy:  "id",
			value:     "new_value_by_id",
			namespace: "test",
		},
		{
			name:      "Access by ENV_NAME",
			accessBy:  "env_name",
			value:     "new_value_by_env",
			namespace: "test",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a context with PID
			pid := registry.ParseID(tc.namespace + ":ns")
			ctx = pubsub.WithPID(ctx, pubsub.PID{ID: pid})

			// Set the value
			var setName string
			if tc.accessBy == "id" {
				setName = tc.namespace + ":test_var"
			} else {
				setName = "TEST_VAR"
			}

			err := reg.Set(ctx, setName, tc.value)
			require.NoError(t, err)

			// Verify we can get the value using both methods
			// First by ID
			valueByID, err := reg.Get(ctx, tc.namespace+":test_var")
			require.NoError(t, err)
			assert.Equal(t, tc.value, valueByID)

			// Then by ENV_NAME
			valueByEnv, err := reg.Get(ctx, "TEST_VAR")
			require.NoError(t, err)
			assert.Equal(t, tc.value, valueByEnv)
		})
	}
}

func TestReadOnlyVariable(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	//nolint:errcheck // ok for tests
	defer logger.Sync()

	ctx := context.Background()
	bus := eventbus.NewBus()
	defer bus.Stop()

	reg := NewRegistry(bus, logger)
	err = reg.Start(ctx)
	require.NoError(t, err)
	//nolint:errcheck // ok for tests
	defer reg.Stop()

	// Create a memory storage
	memStorage := serviceenv.NewMemoryStorage(map[string]string{
		"TEST_VAR": "initial_value",
	}, logger)

	// Register storage
	storageEvt := event.Event{
		System: env.System,
		Kind:   env.StorageRegister,
		Path:   "test:mock-storage",
		Data:   memStorage,
	}
	bus.Send(ctx, storageEvt)
	time.Sleep(100 * time.Millisecond)

	// Register a read-only variable
	variable := env.Variable{
		Name:         "test_var",
		EnvName:      "TEST_VAR",
		StorageID:    "test:mock-storage",
		DefaultValue: "default_value",
		ReadOnly:     true,
	}
	varEvt := event.Event{
		System: env.System,
		Kind:   env.VariableRegister,
		Path:   "test:test_var",
		Data:   variable,
	}
	bus.Send(ctx, varEvt)
	time.Sleep(100 * time.Millisecond)

	// Create a context with PID
	pid := registry.ParseID("test:ns")
	ctx = pubsub.WithPID(ctx, pubsub.PID{ID: pid})

	// Try to set value by ID
	err = reg.Set(ctx, "test:test_var", "new_value")
	require.Error(t, err, "should fail to update read-only variable by ID")
	assert.Equal(t, env.ErrVariableReadOnly, err)

	// Try to set value by ENV_NAME
	err = reg.Set(ctx, "TEST_VAR", "new_value")
	require.Error(t, err, "should fail to update read-only variable by ENV_NAME")
	assert.Equal(t, env.ErrVariableReadOnly, err)

	// Verify variable was not updated using both methods
	valueByID, err := reg.Get(ctx, "test:test_var")
	require.NoError(t, err)
	assert.Equal(t, "initial_value", valueByID)

	valueByEnv, err := reg.Get(ctx, "TEST_VAR")
	require.NoError(t, err)
	assert.Equal(t, "initial_value", valueByEnv)
}
