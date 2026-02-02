package metrics

import (
	"sync"
	"sync/atomic"
	"time"

	api "github.com/wippyai/runtime/api/metrics"
	apicfg "github.com/wippyai/runtime/api/service/metrics"
)

type collector struct {
	recordCh  chan recordEvent
	stopCh    chan struct{}
	exporters []api.Exporter
	cfg       apicfg.Config
	wg        sync.WaitGroup
	dropped   atomic.Uint64
	exportMu  sync.RWMutex
	closed    atomic.Bool
}

type recordEvent struct {
	labels api.Labels
	name   string
	typ    api.MetricType
	value  float64
}

func NewCollector(cfg apicfg.Config) api.Collector {
	_ = cfg.Validate()
	c := &collector{
		cfg:       cfg,
		exporters: make([]api.Exporter, 0),
		recordCh:  make(chan recordEvent, cfg.BufferSize()),
		stopCh:    make(chan struct{}),
	}
	c.wg.Add(1)
	go c.exportLoop()
	return c
}

func (c *collector) CounterInc(name string, labels api.Labels) {
	c.record(name, api.TypeCounter, 1, labels)
}

func (c *collector) CounterAdd(name string, delta float64, labels api.Labels) {
	c.record(name, api.TypeCounter, delta, labels)
}

func (c *collector) GaugeSet(name string, value float64, labels api.Labels) {
	c.record(name, api.TypeGauge, value, labels)
}

func (c *collector) GaugeInc(name string, labels api.Labels) {
	c.record(name, api.TypeGauge, 1, labels)
}

func (c *collector) GaugeDec(name string, labels api.Labels) {
	c.record(name, api.TypeGauge, -1, labels)
}

func (c *collector) HistogramObserve(name string, value float64, labels api.Labels) {
	c.record(name, api.TypeHistogram, value, labels)
}

func (c *collector) record(name string, typ api.MetricType, value float64, labels api.Labels) {
	if c.closed.Load() {
		c.dropped.Add(1)
		return
	}
	select {
	case c.recordCh <- recordEvent{name: name, typ: typ, value: value, labels: labels}:
	default:
		c.dropped.Add(1)
	}
}

// Dropped returns the number of metrics that were dropped due to buffer overflow.
func (c *collector) Dropped() uint64 {
	return c.dropped.Load()
}

func (c *collector) RegisterExporter(e api.Exporter) error {
	c.exportMu.Lock()
	defer c.exportMu.Unlock()
	c.exporters = append(c.exporters, e)
	return nil
}

func (c *collector) exportLoop() {
	defer c.wg.Done()
	batch := make([]recordEvent, 0, 100)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case ev := <-c.recordCh:
			batch = append(batch, ev)
			if len(batch) >= 100 {
				c.flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				c.flush(batch)
				batch = batch[:0]
			}
		case <-c.stopCh:
			close(c.recordCh)
			for ev := range c.recordCh {
				batch = append(batch, ev)
			}
			if len(batch) > 0 {
				c.flush(batch)
			}
			return
		}
	}
}

func (c *collector) flush(batch []recordEvent) {
	c.exportMu.RLock()
	exporters := c.exporters
	c.exportMu.RUnlock()

	for _, ev := range batch {
		for _, e := range exporters {
			_ = e.Record(ev.name, ev.typ, ev.value, ev.labels)
		}
	}
}

func (c *collector) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(c.stopCh)
	c.wg.Wait()

	c.exportMu.RLock()
	exporters := c.exporters
	c.exportMu.RUnlock()

	for _, e := range exporters {
		_ = e.Close()
	}
	return nil
}
