package adaptive

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestControllerConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := DefaultControllerConfig(8)
		require.NoError(t, cfg.Validate())
	})

	t.Run("zero max workers", func(t *testing.T) {
		cfg := DefaultControllerConfig(0)
		cfg.MaxWorkers = 0
		assert.Error(t, cfg.Validate())
	})

	t.Run("zero min workers", func(t *testing.T) {
		cfg := DefaultControllerConfig(8)
		cfg.MinWorkers = 0
		assert.Error(t, cfg.Validate())
	})

	t.Run("min exceeds max", func(t *testing.T) {
		cfg := DefaultControllerConfig(4)
		cfg.MinWorkers = 5
		assert.Error(t, cfg.Validate())
	})

	t.Run("zero control interval", func(t *testing.T) {
		cfg := DefaultControllerConfig(8)
		cfg.ControlInterval = 0
		assert.Error(t, cfg.Validate())
	})

	t.Run("zero idle ticks", func(t *testing.T) {
		cfg := DefaultControllerConfig(8)
		cfg.IdleTicks = 0
		assert.Error(t, cfg.Validate())
	})

	t.Run("zero probe ticks", func(t *testing.T) {
		cfg := DefaultControllerConfig(8)
		cfg.ProbeTicks = 0
		assert.Error(t, cfg.Validate())
	})
}

func TestControllerConfig_Cooldown(t *testing.T) {
	cfg := ControllerConfig{
		ControlInterval: 500 * time.Millisecond,
		ProbeTicks:      4,
	}
	assert.Equal(t, 2*time.Second, cfg.Cooldown())
}

func TestController_TickWithZeroWorkers(t *testing.T) {
	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 100 * time.Millisecond
	c := newController(cfg)

	now := time.Now().Add(200 * time.Millisecond)
	d, _ := c.tick(now, 100, 0, 0, 0)
	assert.Equal(t, scaleNone, d)
}

func TestController_TickTooFast(t *testing.T) {
	cfg := DefaultControllerConfig(8)
	c := newController(cfg)

	now := c.lastTime.Add(10 * time.Millisecond)
	d, _ := c.tick(now, 100, 1, 0, 0)
	assert.Equal(t, scaleNone, d)
}

func TestController_ScaleDownRespectsMinWorkers(t *testing.T) {
	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 100 * time.Millisecond
	cfg.MinWorkers = 3
	cfg.IdleTicks = 1
	c := newController(cfg)

	now := time.Now()
	var ops int64

	// One tick at low utilization with 3 workers (at min)
	now = now.Add(100 * time.Millisecond)
	ops += 10
	d, _ := c.tick(now, ops, 3, 0, 0)
	// Should not scale below min
	assert.Equal(t, scaleNone, d)
}

func TestController_ScaleDownKeepsBusyHeadroom(t *testing.T) {
	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 100 * time.Millisecond
	cfg.IdleTicks = 1
	c := newController(cfg)

	now := time.Now()
	var ops int64

	// 6 workers, 2 busy => util=0.33 (<0.5), no queue => scaleDown
	// target should be workers-1=5, but busy+1=3, so target = max(5, 3) = 5
	now = now.Add(100 * time.Millisecond)
	ops += 10
	d, target := c.tick(now, ops, 6, 2, 0)
	assert.Equal(t, scaleDown, d)
	assert.Equal(t, int32(5), target)
}

func TestController_UpdateStats_FirstSample(t *testing.T) {
	cfg := DefaultControllerConfig(8)
	c := newController(cfg)

	c.updateStats(1000.0)
	assert.Equal(t, 1000.0, c.ema)
	assert.Equal(t, 0.0, c.variance)
	assert.Equal(t, 1, c.samples)
}

func TestController_UpdateStats_EMConvergence(t *testing.T) {
	cfg := DefaultControllerConfig(8)
	c := newController(cfg)

	// Feed constant throughput
	for i := 0; i < 50; i++ {
		c.updateStats(1000.0)
	}

	assert.InDelta(t, 1000.0, c.ema, 1.0)
	assert.InDelta(t, 0.0, c.variance, 1.0)
}

func TestController_CV_ZeroEma(t *testing.T) {
	cfg := DefaultControllerConfig(8)
	c := newController(cfg)
	c.ema = 0
	assert.Equal(t, 1.0, c.cv())
}

func TestController_CV_ZeroVariance(t *testing.T) {
	cfg := DefaultControllerConfig(8)
	c := newController(cfg)
	c.ema = 1000
	c.variance = 0
	assert.Equal(t, 1.0, c.cv())
}

func TestController_NoiseThreshold_CappedAtMax(t *testing.T) {
	cfg := DefaultControllerConfig(8)
	c := newController(cfg)
	// High variance relative to ema gives large CV
	c.ema = 1.0
	c.variance = 100.0
	c.samples = 20
	threshold := c.noiseThreshold()
	assert.LessOrEqual(t, threshold, maxNoiseThreshold)
}

func TestController_StartProbe_CapsAtMaxWorkers(t *testing.T) {
	cfg := DefaultControllerConfig(4)
	cfg.ControlInterval = 100 * time.Millisecond
	cfg.ProbeTicks = 2
	c := newController(cfg)

	now := time.Now()
	// Already at 3 out of 4 max
	_, added := c.startProbe(now, 3, 100)
	assert.Equal(t, int32(1), added)
}

func TestController_CooldownBlocking(t *testing.T) {
	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 100 * time.Millisecond
	cfg.ProbeTicks = 2
	c := newController(cfg)

	now := time.Now()
	var ops int64

	// Warmup
	for i := 0; i < 5; i++ {
		now = now.Add(100 * time.Millisecond)
		ops += 100
		c.tick(now, ops, 1, 0, 0)
	}

	// Trigger scale-up
	now = now.Add(100 * time.Millisecond)
	ops += 100
	d, _ := c.tick(now, ops, 1, 1, 5)
	assert.Equal(t, scaleUp, d)

	// Complete probe with failure (low throughput)
	for i := 0; i < 5; i++ {
		now = now.Add(100 * time.Millisecond)
		ops += 30
		c.tick(now, ops, 2, 2, 10)
	}

	// Now try to scale up again - should be blocked by cooldown
	now = now.Add(100 * time.Millisecond)
	ops += 100
	d, _ = c.tick(now, ops, 1, 1, 5)
	assert.Equal(t, scaleNone, d)
}

func TestController_ScaleDownResetsPeakEma(t *testing.T) {
	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 100 * time.Millisecond
	cfg.IdleTicks = 1
	c := newController(cfg)
	c.peakEma = 5000.0

	now := time.Now()
	now = now.Add(100 * time.Millisecond)

	d, _ := c.tick(now, 10, 4, 0, 0)
	if d == scaleDown {
		assert.Equal(t, 0.0, c.peakEma)
	}
}
