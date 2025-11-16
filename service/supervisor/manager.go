package supervisor

import (
	"context"
	"fmt"
	"sync"

	processapi "github.com/wippyai/runtime/api/service/supervisor"
	entryutil "github.com/wippyai/runtime/internal/entry"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/system/process"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
	"go.uber.org/zap"
)

// ServiceFactory is an interface for creating service instances
type ServiceFactory interface {
	// CreateService creates a new service instance with the given configuration
	CreateService(id registry.ID, config processapi.ServiceConfig) supervisor.Service
}

// Manager handles process services lifecycle and monitoring
type Manager struct {
	log      *zap.Logger
	bus      event.Bus
	proc     *process.Manager
	services sync.Map // map[registry.ID]supervisor.Service
	factory  ServiceFactory
	pidGen   *uniqid.PIDGenerator
}

// NewSupervisorServiceManager creates a new process service manager
func NewSupervisorServiceManager(
	bus event.Bus,
	proc *process.Manager,
	log *zap.Logger,
	pidGen *uniqid.PIDGenerator,
) *Manager {
	return &Manager{
		log:     log,
		bus:     bus,
		proc:    proc,
		factory: NewDefaultServiceFactory(pidGen),
		pidGen:  pidGen,
	}
}

// NewSupervisorServiceManagerWithFactory creates a new process service manager with factory
func NewSupervisorServiceManagerWithFactory(
	bus event.Bus,
	proc *process.Manager,
	log *zap.Logger,
	factory ServiceFactory,
) *Manager {
	return &Manager{
		log:     log,
		bus:     bus,
		proc:    proc,
		factory: factory,
	}
}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != processapi.KindProcessService {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, processapi.KindProcessService)
	}

	// Info: Log entry details before transcoding
	m.log.Info("process supervisor add - entry details",
		zap.String("id", entry.ID.String()),
		zap.String("kind", entry.Kind),
		zap.Any("meta", entry.Meta),
		zap.Bool("has_data", entry.Data != nil),
	)

	// Info: Log payload details if available
	if entry.Data != nil {
		m.log.Info("process supervisor add - payload details",
			zap.String("id", entry.ID.String()),
			zap.String("format", string(entry.Data.Format())),
			zap.Any("data_type", fmt.Sprintf("%T", entry.Data.Data())),
			zap.Any("data_preview", m.previewData(entry.Data.Data())),
		)
	}

	// Get transcoder and log its availability
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		m.log.Error("process supervisor add - no transcoder found in context",
			zap.String("id", entry.ID.String()),
		)
		return fmt.Errorf("no transcoder found in context")
	}
	m.log.Info("process supervisor add - transcoder available",
		zap.String("id", entry.ID.String()),
		zap.String("transcoder_type", fmt.Sprintf("%T", dtt)),
		zap.Any("entry", entry),
	)

	// Unmarshal config
	cfg, err := entryutil.DecodeEntryConfig[processapi.ServiceConfig](ctx, dtt, entry)
	if err != nil {
		m.log.Error("process supervisor add - config decode failed",
			zap.String("id", entry.ID.String()),
			zap.Error(err),
		)
		return err
	}

	// Info: Log decoded config details
	m.log.Info("process supervisor add - config decoded successfully",
		zap.String("id", entry.ID.String()),
		zap.String("process", cfg.Process.String()),
		zap.String("host_id", cfg.HostID),
		zap.Int("input_count", len(cfg.Input)),
		zap.Any("lifecycle", cfg.Lifecycle),
	)

	cfg.Process = cfg.Process.WithDefaultNS(entry.ID.NS)

	// Create service instance
	var svc supervisor.Service
	if m.factory != nil {
		svc = m.factory.CreateService(entry.ID, *cfg)
	} else {
		svc = &Service{
			id:     entry.ID,
			config: *cfg,
			status: make(chan any, 1),
			pidGen: m.pidGen,
		}
	}

	// Store service
	m.services.Store(entry.ID, svc)

	// Register with supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: svc,
			Config:  cfg.Lifecycle,
		},
	})

	m.log.Info("process supervisor added", zap.String("id", entry.ID.String()))

	return nil
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != processapi.KindProcessService {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, processapi.KindProcessService)
	}

	// Info: Log entry details before transcoding
	m.log.Info("process supervisor update - entry details",
		zap.String("id", entry.ID.String()),
		zap.String("kind", entry.Kind),
		zap.Any("meta", entry.Meta),
		zap.Bool("has_data", entry.Data != nil),
	)

	// Info: Log payload details if available
	if entry.Data != nil {
		m.log.Info("process supervisor update - payload details",
			zap.String("id", entry.ID.String()),
			zap.String("format", string(entry.Data.Format())),
			zap.String("data_type", fmt.Sprintf("%T", entry.Data.Data())),
			zap.Any("data_preview", m.previewData(entry.Data.Data())),
		)
	}

	// Get existing service
	_, exists := m.services.Load(entry.ID)
	if !exists {
		return fmt.Errorf("service %s not found", entry.ID)
	}

	// Get transcoder and log its availability
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		m.log.Error("process supervisor update - no transcoder found in context",
			zap.String("id", entry.ID.String()),
		)
		return fmt.Errorf("no transcoder found in context")
	}
	m.log.Info("process supervisor update - transcoder available",
		zap.String("id", entry.ID.String()),
		zap.String("transcoder_type", fmt.Sprintf("%T", dtt)),
	)

	// Unmarshal new config
	cfg, err := entryutil.DecodeEntryConfig[processapi.ServiceConfig](ctx, dtt, entry)
	if err != nil {
		m.log.Error("process supervisor update - config decode failed",
			zap.String("id", entry.ID.String()),
			zap.Error(err),
		)
		return err
	}

	// Info: Log decoded config details
	m.log.Info("process supervisor update - config decoded successfully",
		zap.String("id", entry.ID.String()),
		zap.String("process", cfg.Process.String()),
		zap.String("host_id", cfg.HostID),
		zap.Int("input_count", len(cfg.Input)),
		zap.Any("lifecycle", cfg.Lifecycle),
	)

	cfg.Process = cfg.Process.WithDefaultNS(entry.ID.NS)

	// Update supervisor config
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Update,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Config: cfg.Lifecycle,
		},
	})

	m.log.Info("process supervisor updated", zap.String("id", entry.ID.String()))

	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != processapi.KindProcessService {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, processapi.KindProcessService)
	}

	// Done from supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   entry.ID.String(),
	})

	// Done from services map
	m.services.Delete(entry.ID)

	m.log.Info("process supervisor removed", zap.String("id", entry.ID.String()))

	return nil
}

// previewData safely previews data for debugging purposes
func (m *Manager) previewData(data interface{}) interface{} {
	if data == nil {
		return nil
	}

	// Convert to string for preview, but limit length
	switch v := data.(type) {
	case string:
		if len(v) > 200 {
			return v[:200] + "..."
		}
		return v
	case []byte:
		if len(v) > 200 {
			return string(v[:200]) + "..."
		}
		return string(v)
	case map[string]interface{}:
		// For maps, just show the keys
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		return map[string]interface{}{"keys": keys, "count": len(v)}
	case []interface{}:
		// For slices, just show the length and first few elements
		limit := 3
		if len(v) < limit {
			limit = len(v)
		}
		preview := make([]interface{}, 0, limit)
		for i := 0; i < limit; i++ {
			preview = append(preview, m.previewData(v[i]))
		}
		return map[string]interface{}{"preview": preview, "count": len(v)}
	default:
		// For other types, try to convert to string but limit length
		str := fmt.Sprintf("%v", v)
		if len(str) > 200 {
			return str[:200] + "..."
		}
		return str
	}
}
