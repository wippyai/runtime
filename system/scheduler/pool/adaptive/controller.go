// SPDX-License-Identifier: MPL-2.0

package adaptive

// Adaptive pool controller using probe-based scaling.
//
// Scale-up: multiplicative (add 25% of current workers)
// Scale-down: additive (remove one worker at a time)
//
// Probe validation:
//   - Measure throughput change after scaling
//   - Use statistical significance for noise tolerance
//   - Track peak throughput to detect drift

import (
	"errors"
	"math"
	"math/rand"
	"time"

	"go.uber.org/zap"
)

const (
	// EMA smoothing: EMA = α*sample + (1-α)*EMA
	emaAlpha      = 0.2 // throughput smoothing
	varianceAlpha = 0.1 // variance smoothing (slower for stability)
	warmupSamples = 8   // samples before variance estimate is reliable

	// Hysteresis: gap between thresholds prevents oscillation
	scaleUpUtil   = 1.0 // 100% busy + queue required to scale up
	scaleDownUtil = 0.5 // <50% busy + empty queue to scale down

	// Backoff: cooldown * 2^failCount, max 2^10 = 1024x
	maxBackoffPower = 10
	jitterFraction  = 0.25 // ±25% randomization

	// CV spike detection: reset backoff if CV > 3x stable CV
	cvSpikeThreshold = 3.0

	// Statistical significance: k-sigma (k=2 is ~95% confidence)
	significanceK = 2.0

	// Maximum noise threshold cap (during warmup CV can be very high)
	maxNoiseThreshold = 0.5
)

// ControllerConfig holds tunable parameters.
type ControllerConfig struct {
	MaxWorkers      int
	MinWorkers      int
	ControlInterval time.Duration
	IdleTicks       int // ticks at low util before scale down
	ProbeTicks      int // ticks to measure probe result
}

func DefaultControllerConfig(maxWorkers int) ControllerConfig {
	return ControllerConfig{
		MaxWorkers:      maxWorkers,
		MinWorkers:      1,
		ControlInterval: 500 * time.Millisecond,
		IdleTicks:       8,
		ProbeTicks:      4,
	}
}

func (c ControllerConfig) Cooldown() time.Duration {
	return time.Duration(c.ProbeTicks) * c.ControlInterval
}

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

type scaleDecision int

const (
	scaleNone scaleDecision = iota
	scaleUp
	scaleDown
	probeSuccess
	probeFail
)

// controller implements adaptive scaling logic.
type controller struct {
	lastTime      time.Time
	probeStart    time.Time
	cooldownUntil time.Time
	log           *zap.Logger
	ControllerConfig
	variance      float64
	lastOps       int64
	samples       int
	failCount     int
	idleCount     int
	peakEma       float64
	ema           float64
	baselineTput  float64
	baselineQueue int
	stableCV      float64
	cooldown      time.Duration
	workersAdded  int32
	lastFailedAt  int32
	workersBefore int32
	probing       bool
}

func newController(cfg ControllerConfig) *controller {
	return &controller{
		ControllerConfig: cfg,
		cooldown:         cfg.Cooldown(),
		lastTime:         time.Now(),
	}
}

// tick evaluates metrics and returns scaling decision.
func (c *controller) tick(now time.Time, ops int64, workers, busy int32, queueLen int) (scaleDecision, int32) {
	if workers <= 0 {
		return scaleNone, 0
	}

	// Calculate throughput
	elapsed := now.Sub(c.lastTime).Seconds()
	if elapsed < 0.05 {
		return scaleNone, 0
	}

	throughput := float64(ops-c.lastOps) / elapsed
	c.lastOps = ops
	c.lastTime = now

	c.updateStats(throughput)

	util := float64(busy) / float64(workers)

	// Scale down check (not blocked by cooldown)
	if util < scaleDownUtil && queueLen == 0 && workers > int32(c.MinWorkers) {
		c.idleCount++
		if c.idleCount >= c.IdleTicks {
			c.idleCount = 0
			return c.decideScaleDown(now, workers, busy)
		}
		return scaleNone, 0
	}
	c.idleCount = 0

	// In cooldown? (only blocks scale-up)
	if now.Before(c.cooldownUntil) {
		return scaleNone, 0
	}

	// Currently probing?
	if c.probing {
		return c.evaluateProbe(now, queueLen)
	}

	// Scale up check
	if util >= scaleUpUtil && queueLen > 0 && workers < int32(c.MaxWorkers) {
		return c.startProbe(now, workers, queueLen)
	}

	return scaleNone, 0
}

// updateStats maintains EMA and variance estimates.
func (c *controller) updateStats(throughput float64) {
	if c.samples == 0 {
		c.ema = throughput
		c.variance = 0
	} else {
		deviation := throughput - c.ema
		c.ema = emaAlpha*throughput + (1-emaAlpha)*c.ema
		c.variance = varianceAlpha*(deviation*deviation) + (1-varianceAlpha)*c.variance
	}
	c.samples++
}

// cv returns coefficient of variation (noise level).
func (c *controller) cv() float64 {
	if c.ema <= 0 || c.variance <= 0 {
		return 1.0
	}
	return math.Sqrt(c.variance) / c.ema
}

// noiseThreshold returns statistical significance threshold (k * CV).
func (c *controller) noiseThreshold() float64 {
	threshold := significanceK * c.cv()
	if threshold > maxNoiseThreshold {
		return maxNoiseThreshold
	}
	return threshold
}

// startProbe initiates scaling up with multiplicative increase.
func (c *controller) startProbe(now time.Time, workers int32, queueLen int) (scaleDecision, int32) {
	c.probing = true
	c.probeStart = now
	c.baselineTput = c.ema
	c.baselineQueue = queueLen
	c.workersBefore = workers

	// Two scaling formulas, take maximum:
	// 1. Queue pressure: items per worker (fast cold start)
	// 2. Percentage: 25% of current workers (sustained growth)
	queueBased := int32(0)
	if queueLen > 0 && queueLen > int(workers) {
		if queueLen <= (1<<31 - 1) {
			queueBased = int32(queueLen) / workers
		}
	}

	percentBased := workers / 4
	if percentBased < 1 {
		percentBased = 1
	}

	workersToAdd := queueBased
	if percentBased > workersToAdd {
		workersToAdd = percentBased
	}

	// Cap at 25% of max capacity per probe
	maxStep := int32(c.MaxWorkers) / 4
	if maxStep < 1 {
		maxStep = 1
	}
	if workersToAdd > maxStep {
		workersToAdd = maxStep
	}

	// Cap at remaining capacity
	remaining := int32(c.MaxWorkers) - workers
	if workersToAdd > remaining {
		workersToAdd = remaining
	}

	c.workersAdded = workersToAdd

	if c.log != nil {
		c.log.Debug("probe: scaling up",
			zap.Int32("from", workers),
			zap.Int32("to", workers+workersToAdd),
			zap.Int32("adding", workersToAdd),
			zap.Int("queue", queueLen),
			zap.Float64("throughput", c.ema),
			zap.Float64("cv", c.cv()))
	}

	return scaleUp, workersToAdd
}

// evaluateProbe checks if the added workers helped.
func (c *controller) evaluateProbe(now time.Time, queueLen int) (scaleDecision, int32) {
	cv := c.cv()

	// Noise-adaptive measurement window: scale with CV² for constant precision
	windowMultiplier := 1.0
	if c.samples >= warmupSamples {
		windowMultiplier = 1.0 + math.Min(cv*cv, 3.0)
	}

	requiredDuration := time.Duration(float64(c.cooldown) * windowMultiplier)
	if now.Sub(c.probeStart) < requiredDuration {
		return scaleNone, 0
	}

	c.probing = false

	if c.workersBefore <= 0 || c.baselineTput <= 0 {
		c.setCooldown(now, false)
		return probeFail, c.workersAdded
	}

	improvement := (c.ema - c.baselineTput) / c.baselineTput
	queueImproved := queueLen < c.baselineQueue
	threshold := c.noiseThreshold()

	// Drift detection: have we fallen significantly below peak?
	drifted := c.peakEma > 0 && c.ema < c.peakEma*(1-threshold)

	// Three-tier decision:
	// 1. Drifted from peak: fail (cumulative degradation)
	// 2. Significant improvement: success
	// 3. Significant degradation: fail
	// 4. Noise band: use queue as ground truth
	var success bool
	switch {
	case drifted:
		success = false
	case improvement > threshold:
		success = true
	case improvement < -threshold:
		success = false
	default:
		success = queueImproved
	}

	if success {
		c.failCount = 0
		c.stableCV = 0
		c.setCooldown(now, true)

		if c.ema > c.peakEma {
			c.peakEma = c.ema
		}

		if c.log != nil {
			c.log.Debug("probe: success",
				zap.Int32("workers", c.workersBefore+c.workersAdded),
				zap.Int32("added", c.workersAdded),
				zap.Float64("improvement", improvement),
				zap.Float64("cv", cv),
				zap.Int("queue", queueLen),
				zap.Float64("peak", c.peakEma))
		}
		return probeSuccess, 0
	}

	// Failed: apply exponential backoff with CV spike detection
	if c.lastFailedAt == c.workersBefore+c.workersAdded {
		if c.stableCV > 0 && cv > c.stableCV*cvSpikeThreshold {
			c.failCount = 1
			c.stableCV = cv
		} else {
			c.failCount++
			if c.stableCV == 0 || cv < c.stableCV {
				c.stableCV = cv
			}
		}
	} else {
		c.failCount = 1
		c.lastFailedAt = c.workersBefore + c.workersAdded
		c.stableCV = cv
	}
	c.setCooldown(now, false)

	if c.log != nil {
		c.log.Debug("probe: failed",
			zap.Int32("workers", c.workersBefore),
			zap.Int32("removing", c.workersAdded),
			zap.Float64("improvement", improvement),
			zap.Float64("cv", cv),
			zap.Int("queue", queueLen),
			zap.Float64("peak", c.peakEma),
			zap.Bool("drifted", drifted),
			zap.Int("backoff", 1<<min(c.failCount, maxBackoffPower)))
	}

	return probeFail, c.workersAdded
}

// decideScaleDown implements additive decrease (conservative).
func (c *controller) decideScaleDown(now time.Time, workers, busy int32) (scaleDecision, int32) {
	// Additive decrease: remove 1 worker at a time (conservative)
	target := workers - 1

	// Keep at least busy + 1 for headroom
	if target < busy+1 {
		target = busy + 1
	}

	if target < int32(c.MinWorkers) {
		target = int32(c.MinWorkers)
	}

	if target >= workers {
		return scaleNone, 0
	}

	c.cooldownUntil = now.Add(c.cooldown)
	c.peakEma = 0 // reset peak for new regime

	if c.log != nil {
		c.log.Debug("scaling down",
			zap.Int32("from", workers),
			zap.Int32("to", target),
			zap.Int32("busy", busy))
	}

	return scaleDown, target
}

// setCooldown applies cooldown with exponential backoff and jitter.
func (c *controller) setCooldown(now time.Time, success bool) {
	var duration time.Duration
	if success {
		duration = c.cooldown / 2
	} else {
		multiplier := 1 << min(c.failCount, maxBackoffPower)
		duration = c.cooldown * time.Duration(multiplier)
	}

	//nolint:gosec // G404: weak random acceptable for jitter
	jitter := time.Duration(float64(duration) * jitterFraction * (rand.Float64()*2 - 1))
	c.cooldownUntil = now.Add(duration + jitter)
}
