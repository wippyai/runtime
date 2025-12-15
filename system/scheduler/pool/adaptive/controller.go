package adaptive

// Probe-based adaptive pool controller - 5 parameters only.
//
// Parameters:
//   1. maxWorkers      - Maximum workers (required)
//   2. minWorkers      - Minimum workers (default: 1)
//   3. controlInterval - How often to evaluate (default: 500ms)
//   4. idleTicks       - Idle ticks before scale-down (default: 8)
//   5. probeTicks      - Ticks to evaluate probe result (default: 4)
//
// Core algorithm:
//   1. Track throughput via EMA and variance for noise estimation
//   2. Scale up: all workers busy + queue backlog → add 1 worker (probe)
//   3. Probe success: improvement statistically significant given noise
//   4. Probe fail: remove the added worker
//   5. Scale down: low utilization sustained → gradual (max 50% reduction)
//
// Adaptive behavior:
//   - At low worker counts: use throughput improvement (signal > noise)
//   - At high worker counts: use queue behavior (noise > signal)
//   - Variance tracking detects when to switch strategies

import (
	"errors"
	"math"
	"time"

	"go.uber.org/zap"
)

// Derived constants for the controller algorithm.
const (
	emaAlpha              = 0.2 // balances responsiveness with stability
	varianceAlpha         = 0.1 // slower adaptation for variance (more stable)
	minBaselineThroughput = 1.0 // minimum ops/sec for reliable probe
	minSNR                = 2.0 // minimum signal-to-noise ratio for throughput-based decisions
	warmupSamples         = 10  // samples needed before trusting variance estimate
)

// ControllerConfig holds the 5 configuration parameters.
type ControllerConfig struct {
	MaxWorkers      int
	MinWorkers      int           // default: 1
	ControlInterval time.Duration // default: 500ms
	IdleTicks       int           // default: 8
	ProbeTicks      int           // default: 4
}

// DefaultControllerConfig returns configuration with sensible defaults.
func DefaultControllerConfig(maxWorkers int) ControllerConfig {
	return ControllerConfig{
		MaxWorkers:      maxWorkers,
		MinWorkers:      1,
		ControlInterval: 500 * time.Millisecond,
		IdleTicks:       8,
		ProbeTicks:      4,
	}
}

// Cooldown returns the probe cooldown duration.
func (c ControllerConfig) Cooldown() time.Duration {
	return time.Duration(c.ProbeTicks) * c.ControlInterval
}

// Validate checks configuration values are valid.
func (c ControllerConfig) Validate() error {
	if c.MaxWorkers <= 0 {
		return errors.New("maxWorkers must be positive")
	}
	if c.MinWorkers <= 0 {
		return errors.New("minWorkers must be positive")
	}
	if c.MinWorkers > c.MaxWorkers {
		return errors.New("minWorkers cannot exceed maxWorkers")
	}
	if c.ControlInterval <= 0 {
		return errors.New("controlInterval must be positive")
	}
	if c.IdleTicks <= 0 {
		return errors.New("idleTicks must be positive")
	}
	if c.ProbeTicks <= 0 {
		return errors.New("probeTicks must be positive")
	}
	return nil
}

// scaleDecision represents what the controller wants to do.
type scaleDecision int

const (
	scaleNone scaleDecision = iota
	scaleUp
	scaleDown
	probeSuccess
	probeFail
)

// controller implements the probe-based scaling logic.
type controller struct {
	ControllerConfig

	// Derived
	cooldown time.Duration

	// State
	ema           float64
	variance      float64 // EMA of squared deviations (for noise estimation)
	samples       int     // number of samples collected (for warmup)
	lastOps       int64
	lastTime      time.Time
	cooldownUntil time.Time
	idleCount     int

	// Probe state
	probing            bool
	probeStart         time.Time
	probeOps           int64   // ops count at probe start
	baselineThroughput float64 // throughput before probe
	baselineQueue      int     // queue length before probe
	workersBefore      int32

	// Exponential backoff for repeated failures
	consecutiveFails int   // consecutive failures at same worker count
	lastFailedAt     int32 // worker count where we last failed

	// Logger (optional)
	log *zap.Logger
}

func newController(cfg ControllerConfig) *controller {
	return &controller{
		ControllerConfig: cfg,
		cooldown:         cfg.Cooldown(),
		lastTime:         time.Now(),
	}
}

func (c *controller) withLogger(log *zap.Logger) *controller {
	c.log = log
	return c
}

// tick is called every controlInterval with current metrics.
// Returns the scaling decision and target worker count for scale-down.
func (c *controller) tick(now time.Time, ops int64, workers, busy int32, queueLen int) (scaleDecision, int32) {
	// Guard against invalid state
	if workers <= 0 {
		return scaleNone, 0
	}

	// Calculate throughput
	elapsed := now.Sub(c.lastTime).Seconds()
	if elapsed <= 0 || elapsed < 0.05 { // handle clock skew and minimum sample time
		return scaleNone, 0
	}

	throughput := float64(ops-c.lastOps) / elapsed
	c.lastOps = ops
	c.lastTime = now

	// Update EMA and variance
	if c.ema == 0 {
		c.ema = throughput
		c.variance = 0
	} else {
		deviation := throughput - c.ema
		c.ema = emaAlpha*throughput + (1-emaAlpha)*c.ema
		c.variance = varianceAlpha*(deviation*deviation) + (1-varianceAlpha)*c.variance
	}
	c.samples++

	// In cooldown?
	if now.Before(c.cooldownUntil) {
		return scaleNone, 0
	}

	// Utilization
	utilization := float64(busy) / float64(workers)
	allBusy := busy == workers

	// Probing state?
	if c.probing {
		return c.evaluateProbe(now, ops, queueLen)
	}

	// Scale down: low utilization sustained
	if utilization < 0.5 && queueLen == 0 && workers > int32(c.MinWorkers) {
		c.idleCount++
		if c.idleCount >= c.IdleTicks {
			c.idleCount = 0
			// Gradual scale-down: max 50% reduction per decision
			target := workers / 2
			if target < busy+1 {
				target = busy + 1
			}
			if target < int32(c.MinWorkers) {
				target = int32(c.MinWorkers)
			}
			if target < workers {
				c.cooldownUntil = now.Add(c.cooldown / 2)
				if c.log != nil {
					c.log.Info("scaling down",
						zap.Int32("from", workers),
						zap.Int32("to", target),
						zap.Int32("busy", busy),
						zap.Float64("utilization", utilization),
						zap.Float64("throughput", c.ema))
				}
				return scaleDown, target
			}
		}
		return scaleNone, 0
	}
	c.idleCount = 0

	// Scale up: all workers busy with queue backlog
	if allBusy && queueLen > 0 && workers < int32(c.MaxWorkers) {
		c.probing = true
		c.probeStart = now
		c.probeOps = ops
		c.baselineThroughput = throughput
		c.baselineQueue = queueLen
		c.workersBefore = workers
		if c.log != nil {
			c.log.Info("probing scale up",
				zap.Int32("from", workers),
				zap.Int32("to", workers+1),
				zap.Int("queue", queueLen),
				zap.Float64("throughput", c.ema),
				zap.Float64("snr", c.signalToNoise(workers)))
		}
		return scaleUp, 0
	}

	return scaleNone, 0
}

// stddev returns the estimated standard deviation of throughput.
func (c *controller) stddev() float64 {
	if c.variance <= 0 {
		return 0
	}
	return math.Sqrt(c.variance)
}

// signalToNoise returns the ratio of expected signal to noise.
// Signal is the theoretical max improvement (1/workers).
// Noise is the coefficient of variation (stddev/mean).
func (c *controller) signalToNoise(workers int32) float64 {
	if c.ema <= 0 || workers <= 0 {
		return 0
	}
	signal := 1.0 / float64(workers) // theoretical max improvement
	noise := c.stddev() / c.ema      // coefficient of variation
	if noise <= 0 {
		return 100 // very high SNR if no noise observed
	}
	return signal / noise
}

// evaluateProbe checks if the probe (added worker) was successful.
// Uses adaptive strategy based on signal-to-noise ratio:
//   - High SNR: evaluate based on throughput improvement
//   - Low SNR: evaluate based on queue behavior (more reliable at high worker counts)
func (c *controller) evaluateProbe(now time.Time, ops int64, currentQueue int) (scaleDecision, int32) {
	// Wait for probe duration
	if now.Sub(c.probeStart) < c.cooldown {
		return scaleNone, 0
	}

	c.probing = false

	// Guard against invalid state
	probeElapsed := now.Sub(c.probeStart).Seconds()
	if probeElapsed <= 0 || c.workersBefore <= 0 {
		c.cooldownUntil = now.Add(c.cooldown)
		return probeFail, 0
	}

	// Require minimum baseline for reliable measurement
	if c.baselineThroughput < minBaselineThroughput {
		c.cooldownUntil = now.Add(c.cooldown)
		return probeFail, 0
	}

	// Calculate probe throughput from raw ops (not EMA)
	probeThroughput := float64(ops-c.probeOps) / probeElapsed
	improvement := (probeThroughput - c.baselineThroughput) / c.baselineThroughput

	// Theoretical max improvement from adding 1 worker to N workers is 1/N
	theoreticalMax := 1.0 / float64(c.workersBefore)

	// Adaptive evaluation based on signal-to-noise ratio
	snr := c.signalToNoise(c.workersBefore)
	efficiency := improvement / theoreticalMax
	queueImproved := c.baselineQueue > 0 && currentQueue < c.baselineQueue

	// During warmup, variance estimate is unreliable - use throughput mode
	warmingUp := c.samples < warmupSamples
	useThroughputMode := warmingUp || snr >= minSNR

	// Core insight: when we can measure (SNR high), use throughput.
	// When noise dominates (SNR low), use queue behavior but require non-negative throughput.
	var success bool
	if useThroughputMode {
		success = efficiency >= 0.5
	} else {
		// Queue mode: queue must improve AND throughput must not decrease
		success = queueImproved && improvement >= 0
	}

	mode := "throughput"
	if !useThroughputMode {
		mode = "queue"
	}

	if success {
		c.cooldownUntil = now.Add(c.cooldown / 2)
		c.consecutiveFails = 0
		if c.log != nil {
			c.log.Info("probe success",
				zap.Int32("workers", c.workersBefore+1),
				zap.String("mode", mode),
				zap.Float64("efficiency", efficiency),
				zap.Float64("snr", snr),
				zap.Int("queue", currentQueue))
		}
		return probeSuccess, 0
	}

	// Fail: worker isn't helping enough - apply exponential backoff
	if c.lastFailedAt == c.workersBefore+1 {
		c.consecutiveFails++
	} else {
		c.consecutiveFails = 1
		c.lastFailedAt = c.workersBefore + 1
	}

	// Exponential backoff: cooldown * 2^min(fails, 4) caps at 16x
	backoffMultiplier := 1 << min(c.consecutiveFails, 4)
	c.cooldownUntil = now.Add(c.cooldown * time.Duration(backoffMultiplier))

	if c.log != nil {
		c.log.Info("probe failed",
			zap.Int32("workers", c.workersBefore),
			zap.String("mode", mode),
			zap.Float64("efficiency", efficiency),
			zap.Float64("snr", snr),
			zap.Int("queue", currentQueue),
			zap.Int("backoff", backoffMultiplier))
	}
	return probeFail, 0
}
