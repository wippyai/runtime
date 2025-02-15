package metrics

//
//type metricRegistry struct {
//	reg    *prometheus.Registry
//	server *metricServer // Optional HTTP server
//	config *RegistryConfig
//	log    *zap.Logger
//	mu     sync.RWMutex
//
//	// Store metric collectors for direct access
//	counters   map[string]*prometheusCounter
//	gauges     map[string]*prometheusGauge
//	histograms map[string]*prometheusHistogram
//	summaries  map[string]*prometheusSummary
//}
//
//func newMetricRegistry(cfg *RegistryConfig, logger *zap.Logger) *metricRegistry {
//	return &metricRegistry{
//		reg:        prometheus.NewRegistry(),
//		config:     cfg,
//		log:        logger,
//		counters:   make(map[string]*prometheusCounter),
//		gauges:     make(map[string]*prometheusGauge),
//		histograms: make(map[string]*prometheusHistogram),
//		summaries:  make(map[string]*prometheusSummary),
//	}
//}
//
//// Start initializes the HTTP server if configured
//func (r *metricRegistry) Start(ctx context.Context) (<-chan any, error) {
//	r.mu.Lock()
//	defer r.mu.Unlock()
//
//	if r.config.Address != "" {
//		r.server = newMetricServer(r.config, r.reg)
//		return r.server.Start(ctx)
//	}
//
//	// Return nil channel for internal-only usage
//	return nil, nil
//}
//
//// Stop shuts down the HTTP server if running
//func (r *metricRegistry) Stop(ctx context.Context) error {
//	r.mu.Lock()
//	defer r.mu.Unlock()
//
//	if r.server != nil {
//		return r.server.Stop(ctx)
//	}
//	return nil
//}
//
//// GetMetricValue retrieves the current value of a metric by name
//func (r *metricRegistry) GetMetricValue(name string) (float64, map[string]string, error) {
//	r.mu.RLock()
//	defer r.mu.RUnlock()
//
//	// Try to find metric in our stored collectors
//	if counter, ok := r.counters[name]; ok {
//		return counter.getValue()
//	}
//	if gauge, ok := r.gauges[name]; ok {
//		return gauge.getValue()
//	}
//	// Note: Histograms and Summaries require special handling for their multiple values
//
//	return 0, nil, fmt.Errorf("metric %s not found", name)
//}
//
//// GetMetricValues returns all current metric values
//func (r *metricRegistry) GetMetricValues() (map[string]MetricValue, error) {
//	r.mu.RLock()
//	defer r.mu.RUnlock()
//
//	metrics, err := r.reg.Gather()
//	if err != nil {
//		return nil, fmt.Errorf("gathering metrics: %w", err)
//	}
//
//	result := make(map[string]MetricValue)
//	for _, mf := range metrics {
//		name := *mf.Name
//		for _, m := range mf.Metric {
//			labels := makeLabelsMap(m.Label)
//
//			switch *mf.Type {
//			case dto.MetricType_COUNTER:
//				result[name] = MetricValue{
//					Type:   "counter",
//					Value:  *m.Counter.Value,
//					Labels: labels,
//				}
//			case dto.MetricType_GAUGE:
//				result[name] = MetricValue{
//					Type:   "gauge",
//					Value:  *m.Gauge.Value,
//					Labels: labels,
//				}
//				// Add handling for other types as needed
//			}
//		}
//	}
//
//	return result, nil
//}

//
//// Helper to convert label pairs to map
//func makeLabelsMap(pairs []*dto.LabelPair) map[string]string {
//	result := make(map[string]string)
//	for _, pair := range pairs {
//		result[*pair.Name] = *pair.Value
//	}
//	return result
//}
//
//type MetricValue struct {
//	Type   string
//	Value  float64
//	Labels map[string]string
//}
//
//// Counter implementation
//type prometheusCounter struct {
//	counter prometheus.Counter
//	vec     *prometheus.CounterVec
//}
//
//func (r *metricRegistry) Counter(name, help string, labels ...string) Counter {
//	r.mu.Lock()
//	defer r.mu.Unlock()
//
//	if c, exists := r.counters[name]; exists {
//		return c
//	}
//
//	var counter prometheus.Counter
//	var vec *prometheus.CounterVec
//
//	if len(labels) > 0 {
//		vec = prometheus.NewCounterVec(
//			prometheus.CounterOpts{
//				Name: r.config.Prefix + "_" + name,
//				Help: help,
//			},
//			labels,
//		)
//		r.reg.MustRegister(vec)
//		counter = vec.WithLabelValues(make([]string, len(labels))...)
//	} else {
//		counter = prometheus.NewCounter(prometheus.CounterOpts{
//			Name: r.config.Prefix + "_" + name,
//			Help: help,
//		})
//		r.reg.MustRegister(counter)
//	}
//
//	c := &prometheusCounter{counter: counter, vec: vec}
//	r.counters[name] = c
//	return c
//}
//
//func (c *prometheusCounter) Inc(labels map[string]string) {
//	if c.vec != nil {
//		c.vec.With(labels).Inc()
//	} else {
//		c.counter.Inc()
//	}
//}
//
//func (c *prometheusCounter) Add(value float64, labels map[string]string) {
//	if c.vec != nil {
//		c.vec.With(labels).Add(value)
//	} else {
//		c.counter.Add(value)
//	}
//}
//
//func (c *prometheusCounter) getValue() (float64, map[string]string, error) {
//	var m dto.Metric
//	err := c.counter.Write(&m)
//	if err != nil {
//		return 0, nil, err
//	}
//	return *m.Counter.Value, makeLabelsMap(m.Label), nil
//}
//
//// Gauge implementation
//type prometheusGauge struct {
//	gauge prometheus.Gauge
//	vec   *prometheus.GaugeVec
//}
//
//func (r *metricRegistry) Gauge(name, help string, labels ...string) Gauge {
//	r.mu.Lock()
//	defer r.mu.Unlock()
//
//	if g, exists := r.gauges[name]; exists {
//		return g
//	}
//
//	var gauge prometheus.Gauge
//	var vec *prometheus.GaugeVec
//
//	if len(labels) > 0 {
//		vec = prometheus.NewGaugeVec(
//			prometheus.GaugeOpts{
//				Name: r.config.Prefix + "_" + name,
//				Help: help,
//			},
//			labels,
//		)
//		r.reg.MustRegister(vec)
//		gauge = vec.WithLabelValues(make([]string, len(labels))...)
//	} else {
//		gauge = prometheus.NewGauge(prometheus.GaugeOpts{
//			Name: r.config.Prefix + "_" + name,
//			Help: help,
//		})
//		r.reg.MustRegister(gauge)
//	}
//
//	g := &prometheusGauge{gauge: gauge, vec: vec}
//	r.gauges[name] = g
//	return g
//}
//
//func (g *prometheusGauge) Set(value float64, labels map[string]string) {
//	if g.vec != nil {
//		g.vec.With(labels).Set(value)
//	} else {
//		g.gauge.Set(value)
//	}
//}
//
//func (g *prometheusGauge) Inc(labels map[string]string) {
//	if g.vec != nil {
//		g.vec.With(labels).Inc()
//	} else {
//		g.gauge.Inc()
//	}
//}
//
//func (g *prometheusGauge) Dec(labels map[string]string) {
//	if g.vec != nil {
//		g.vec.With(labels).Dec()
//	} else {
//		g.gauge.Dec()
//	}
//}
//
//func (g *prometheusGauge) Add(value float64, labels map[string]string) {
//	if g.vec != nil {
//		g.vec.With(labels).Add(value)
//	} else {
//		g.gauge.Add(value)
//	}
//}
//
//func (g *prometheusGauge) Sub(value float64, labels map[string]string) {
//	if g.vec != nil {
//		g.vec.With(labels).Sub(value)
//	} else {
//		g.gauge.Sub(value)
//	}
//}
//
//func (g *prometheusGauge) getValue() (float64, map[string]string, error) {
//	var m dto.Metric
//	err := g.gauge.Write(&m)
//	if err != nil {
//		return 0, nil, err
//	}
//	return *m.Gauge.Value, makeLabelsMap(m.Label), nil
//}
//
//// Similar implementations for Histogram and Summary...
