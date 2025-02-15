package metrics

//
//type Manager struct {
//	log        *zap.Logger
//	bus        events.Bus
//	registries sync.Map // map[registry.ID]*metricRegistry
//	defaultID  atomic.Pointer[registry.ID]
//}
//
//func NewManager(bus events.Bus, logger *zap.Logger) *Manager {
//	return &Manager{
//		log: logger,
//		bus: bus,
//	}
//}
//
//func (m *Manager) Get(id registry.ID) (metrics.Registry, error) {
//	if reg, ok := m.registries.Load(id); ok {
//		return reg.(metrics.Registry), nil
//	}
//	return nil, fmt.Errorf("registry %s not found", id)
//}
//
//func (m *Manager) GetDefault() (metrics.Registry, error) {
//	if defID := m.defaultID.Load(); defID != nil {
//		return m.Get(*defID)
//	}
//	return nil, fmt.Errorf("no default registry configured")
//}
//
//func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
//	if entry.Data == nil {
//		return fmt.Errorf("configuration data is required for create operation")
//	}
//
//	if entry.Kind != metrics.KindRegistry {
//		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
//	}
//
//	cfg := new(metrics.RegistryConfig)
//	if err := m.unmarshalAndValidate(ctx, entry.Data, cfg); err != nil {
//		return err
//	}
//
//	cfg.InitDefaults()
//
//	reg := newMetricRegistry(cfg, m.log.With(
//		zap.String("registry", entry.ID.String()),
//	))
//
//	if _, loaded := m.registries.LoadOrStore(entry.ID, reg); loaded {
//		return fmt.Errorf("registry %s already exists", entry.ID)
//	}
//
//	if cfg.IsDefault {
//		if old := m.defaultID.Swap(&entry.ID); old != nil {
//			m.log.Warn("overriding existing default registry",
//				zap.String("old", old.String()),
//				zap.String("new", entry.ID.String()))
//		}
//	}
//
//	m.bus.Send(ctx, events.Event{
//		System: supervisor.System,
//		Kind:   supervisor.Register,
//		Path:   entry.ID.String(),
//		Data: &supervisor.Entry{
//			Service: reg,
//			Config:  cfg.Lifecycle,
//		},
//	})
//
//	m.log.Info("registry added",
//		zap.String("id", entry.ID.String()),
//		zap.Bool("default", cfg.IsDefault))
//
//	return nil
//}
//
//func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
//	if entry.Data == nil {
//		return fmt.Errorf("configuration data is required for update operation")
//	}
//
//	if entry.Kind != metrics.KindRegistry {
//		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
//	}
//
//	cfg := new(metrics.RegistryConfig)
//	if err := m.unmarshalAndValidate(ctx, entry.Data, cfg); err != nil {
//		return err
//	}
//
//	cfg.InitDefaults()
//
//	reg, ok := m.registries.Load(entry.ID)
//	if !ok {
//		return fmt.Errorf("registry %s not found", entry.ID)
//	}
//
//	metricReg := reg.(*metricRegistry)
//	if err := metricReg.updateConfig(cfg); err != nil {
//		return fmt.Errorf("failed to update registry: %w", err)
//	}
//
//	m.bus.Send(ctx, events.Event{
//		System: supervisor.System,
//		Kind:   supervisor.Update,
//		Path:   entry.ID.String(),
//		Data:   &supervisor.Entry{Config: cfg.Lifecycle},
//	})
//
//	if cfg.IsDefault {
//		if old := m.defaultID.Swap(&entry.ID); old != nil && *old != entry.ID {
//			m.log.Warn("overriding existing default registry",
//				zap.String("old", old.String()),
//				zap.String("new", entry.ID.String()))
//		}
//	} else if defID := m.defaultID.Load(); defID != nil && *defID == entry.ID {
//		m.defaultID.Store(nil)
//	}
//
//	m.log.Info("registry updated",
//		zap.String("id", entry.ID.String()),
//		zap.Bool("default", cfg.IsDefault))
//
//	return nil
//}
//
//func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
//	if entry.Kind != metrics.KindRegistry {
//		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
//	}
//
//	if defID := m.defaultID.Load(); defID != nil && *defID == entry.ID {
//		m.defaultID.Store(nil)
//	}
//
//	reg, loaded := m.registries.LoadAndDelete(entry.ID)
//	if !loaded {
//		return fmt.Errorf("registry %s not found", entry.ID)
//	}
//
//	m.bus.Send(ctx, events.Event{
//		System: supervisor.System,
//		Kind:   supervisor.Remove,
//		Path:   entry.ID.String(),
//	})
//
//	if err := reg.(*metricRegistry).close(); err != nil {
//		m.log.Error("failed to close registry",
//			zap.String("id", entry.ID.String()),
//			zap.Error(err))
//	}
//
//	m.log.Info("registry removed", zap.String("id", entry.ID.String()))
//	return nil
//}
//
//func (m *Manager) unmarshalAndValidate(ctx context.Context, data payload.Payload, cfg interface{}) error {
//	dtt := payload.GetTranscoder(ctx)
//	if dtt == nil {
//		return fmt.Errorf("missing transcoder in context")
//	}
//
//	if err := dtt.Unmarshal(data, cfg); err != nil {
//		return fmt.Errorf("failed to unmarshal config: %w", err)
//	}
//
//	if validator, ok := cfg.(interface{ Validate() error }); ok {
//		if err := validator.Validate(); err != nil {
//			return fmt.Errorf("invalid configuration: %w", err)
//		}
//	}
//
//	return nil
//}
