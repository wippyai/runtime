package interceptor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewDefaultFactory(t *testing.T) {
	factory := NewDefaultFactory()
	assert.NotNil(t, factory)
	assert.IsType(t, &DefaultFactory{}, factory)
}

func TestDefaultFactory_CreateManager(t *testing.T) {
	factory := NewDefaultFactory()
	bus := &mockEventBus{}
	logger := zap.NewNop()

	manager := factory.CreateManager(bus, logger)

	require.NotNil(t, manager)
	assert.Equal(t, bus, manager.eventBus)
	assert.Equal(t, logger, manager.logger)
}
