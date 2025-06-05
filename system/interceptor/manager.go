package interceptor

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"golang.org/x/time/rate"

	"github.com/ponyruntime/pony/api/interceptor"
	"github.com/ponyruntime/pony/api/payload"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"
)

// Manager handles interceptor lifecycle and resource provisioning
type Manager struct {
	logger   *zap.Logger
	eventBus event.Bus
}

// NewManager creates a new interceptor manager
func NewManager(eventBus event.Bus, logger *zap.Logger) *Manager {
	return &Manager{
		logger:   logger,
		eventBus: eventBus,
	}
}
func (m *Manager) InitInterceptors(ctx context.Context) error {
	// Register default interceptors
	interceptors := []struct {
		name string
		ic   interceptor.Interceptor
	}{
		{
			name: "timeout",
			ic:   NewTimeoutInterceptor(1 * time.Second),
		},
		{
			name: "otel",
			ic:   NewOTelInterceptor(),
		},
		{
			name: "ratelimit",
			ic: NewRateLimitInterceptor(interceptor.RateLimit{
				RequestsPerSecond: 10,
				Burst:             200,
			}, expirable.NewLRU[string, *rate.Limiter](10000, nil, time.Second)),
		},
		{
			name: "retry",
			ic: NewRetryInterceptor(&interceptor.RetryPolicy{
				MaxAttempts: 3,
			}),
		},
	}

	for _, i := range interceptors {
		if err := m.Add(ctx, registry.Entry{
			ID: registry.ID{
				NS:   "interceptor",
				Name: i.name,
			},
			Data: payload.New(i.ic),
		}); err != nil {
			return fmt.Errorf("error adding %s interceptor: %w", i.name, err)
		}
	}

	return nil
}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	ic, ok := entry.Data.Data().(interceptor.Interceptor)
	if !ok {
		return fmt.Errorf("invalid interceptor data type")
	}

	// Register as registry storage
	m.eventBus.Send(ctx, event.Event{
		System: interceptor.System,
		Kind:   interceptor.Register,
		Path:   entry.ID.String(),
		Data:   ic,
	})

	return nil
}

// Update implements registry.EntryListener
func (m *Manager) Update(_ context.Context, entry registry.Entry) error {
	ic, ok := entry.Data.(interceptor.Interceptor)
	if !ok {
		return fmt.Errorf("invalid interceptor data type")
	}

	// Send register event to the registry (same as Add since we don't distinguish)
	m.eventBus.Send(context.Background(), event.Event{
		System: interceptor.System,
		Kind:   interceptor.Update,
		Path:   fmt.Sprintf("%s/%s", entry.ID.NS, entry.ID.Name),
		Data:   ic,
	})

	m.logger.Info("sent interceptor update request",
		zap.String("id", entry.ID.String()))
	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(_ context.Context, entry registry.Entry) error {
	// Send delete event to the registry
	m.eventBus.Send(context.Background(), event.Event{
		System: interceptor.System,
		Kind:   interceptor.Delete,
		Path:   fmt.Sprintf("%s/%s", entry.ID.NS, entry.ID.Name),
	})

	m.logger.Info("sent interceptor deletion request",
		zap.String("id", entry.ID.String()))
	return nil
}
