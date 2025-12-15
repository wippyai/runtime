package adaptive

// Adaptive pool controller.
//
// Scaling:
//   - AIMD: additive increase (+1), multiplicative decrease (50%)
//   - Probe-based: add worker, measure improvement, keep or remove
//
// Stability:
//   - Hysteresis: different thresholds for scale up vs down
//   - Exponential backoff on failed probes
//   - CV spike detection resets backoff when workload changes
//
// Noise handling:
//   - EMA smoothing of throughput measurements
//   - Variance tracking extends measurement window in high noise
//   - Queue behavior as ground truth when throughput is unreliable

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

	// Probe success threshold: baseConfidence / (1 + backlogSeconds/cooldownSeconds)
	baseConfidence = 0.3
	warmupSamples  = 8 // samples before variance estimate is reliable

	// Hysteresis: gap between thresholds prevents oscillation
	scaleUpUtil   = 1.0 // 100% busy + queue required to scale up
	scaleDownUtil = 0.5 // <50% busy + empty queue to scale down

	// Noise thresholds for decision mode
	cvLowNoise    = 0.3 // below: trust throughput
	cvMediumNoise = 0.5 // below: require agreement
	// above cvMediumNoise: queue is primary signal

	// Backoff: cooldown * 2^failCount, max 2^10 = 1024x (~34 min with 2s base)
	maxBackoffPower = 10
	jitterFraction  = 0.25 // ±25% randomization

	// CV spike: if current CV > 2x stable CV, reset backoff (workload changed)
	cvSpikeThreshold = 2.0
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
	ControllerConfig
	cooldown time.Duration
	log      *zap.Logger

	// Throughput tracking (EMA + variance for noise estimation)
	ema      float64
	variance float64
	samples  int
	lastOps  int64
	lastTime time.Time

	// State machine
	cooldownUntil time.Time
	idleCount     int

	// Probe state
	probing       bool
	probeStart    time.Time
	baselineTput  float64
	baselineQueue int
	workersBefore int32

	// Backoff state (per worker-count)
	failCount    int
	lastFailedAt int32
	stableCV     float64 // CV when we stabilized (for spike detection)
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

	// Update EMA and variance
	c.updateStats(throughput)

	// In cooldown?
	if now.Before(c.cooldownUntil) {
		return scaleNone, 0
	}

	// Currently probing?
	if c.probing {
		return c.evaluateProbe(now, queueLen)
	}

	util := float64(busy) / float64(workers)

	// Scale down check: low utilization sustained (hysteresis)
	if util < scaleDownUtil && queueLen == 0 && workers > int32(c.MinWorkers) {
		c.idleCount++
		if c.idleCount >= c.IdleTicks {
			c.idleCount = 0
			return c.decideScaleDown(now, workers, busy, util)
		}
		return scaleNone, 0
	}
	c.idleCount = 0

	// Scale up check: saturated (hysteresis)
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

// stddev returns estimated standard deviation.
func (c *controller) stddev() float64 {
	if c.variance <= 0 {
		return 0
	}
	return math.Sqrt(c.variance)
}

// coefficientOfVariation returns noise level (stddev/mean).
func (c *controller) coefficientOfVariation() float64 {
	if c.ema <= 0 {
		return 1.0 // assume high noise if no data
	}
	return c.stddev() / c.ema
}

// startProbe initiates a probe (add 1 worker).
func (c *controller) startProbe(now time.Time, workers int32, queueLen int) (scaleDecision, int32) {
	c.probing = true
	c.probeStart = now
	c.baselineTput = c.ema
	c.baselineQueue = queueLen
	c.workersBefore = workers

	if c.log != nil {
		c.log.Info("probe: scaling up",
			zap.Int32("from", workers),
			zap.Int32("to", workers+1),
			zap.Int("queue", queueLen),
			zap.Float64("throughput", c.ema),
			zap.Float64("cv", c.coefficientOfVariation()))
	}

	return scaleUp, 0
}

// evaluateProbe checks if the added worker helped.
func (c *controller) evaluateProbe(now time.Time, queueLen int) (scaleDecision, int32) {
	cv := c.coefficientOfVariation()

	// Noise-adaptive measurement window (only after warmup when variance is reliable)
	windowMultiplier := 1.0
	if c.samples >= warmupSamples {
		windowMultiplier = 1.0 + math.Min(cv, 1.0) // 1x to 2x based on noise
	}

	requiredDuration := time.Duration(float64(c.cooldown) * windowMultiplier)
	if now.Sub(c.probeStart) < requiredDuration {
		return scaleNone, 0
	}

	c.probing = false

	if c.workersBefore <= 0 || c.baselineTput <= 0 {
		c.setCooldown(now, false)
		return probeFail, 0
	}

	// Measure improvement using smoothed throughput
	improvement := (c.ema - c.baselineTput) / c.baselineTput

	// Theoretical max improvement from adding 1 worker to N
	theoreticalMax := 1.0 / float64(c.workersBefore)
	efficiency := improvement / theoreticalMax

	// Queue behavior (Little's Law: ground truth in high noise)
	queueImproved := queueLen < c.baselineQueue

	// Dynamic threshold based on queue pressure
	// backlogSeconds = how many seconds of work is queued
	// threshold decreases as backlog grows (more lenient under pressure)
	backlogSeconds := 0.0
	if c.ema > 0 {
		backlogSeconds = float64(c.baselineQueue) / c.ema
	}
	cooldownSeconds := c.cooldown.Seconds()
	threshold := baseConfidence / (1 + backlogSeconds/cooldownSeconds)

	// Decision: combine throughput efficiency and queue behavior
	// In high noise (cv > 0.5), weight queue more heavily
	var success bool
	switch {
	case c.samples < warmupSamples || cv < 0.3:
		// Low noise: trust throughput measurement
		success = efficiency >= threshold
	case cv < 0.5:
		// Medium noise: require both signals to agree
		success = efficiency >= threshold*0.8 && (queueImproved || improvement > 0)
	default:
		// High noise: queue is primary signal, throughput is secondary
		success = queueImproved && improvement >= 0
	}

	if success {
		c.failCount = 0
		c.stableCV = 0 // reset for new worker count
		c.setCooldown(now, true)

		// Aggressive scaling: extra workers based on efficiency/threshold ratio
		// Capped to at most double current workers to prevent overshoot after scale-down
		extraWorkers := int32(0)
		if threshold > 0 && queueLen > 0 {
			ratio := efficiency / threshold
			if ratio >= 2.0 {
				extra := int32(ratio) - 1
				// Cap: don't exceed maxWorkers, and don't more than double
				maxExtra := int32(c.MaxWorkers) - c.workersBefore - 1
				if extra > c.workersBefore {
					extra = c.workersBefore
				}
				if extra > maxExtra {
					extra = maxExtra
				}
				if extra > 0 {
					extraWorkers = extra
				}
			}
		}

		if c.log != nil {
			c.log.Info("probe: success - keeping worker",
				zap.Int32("workers", c.workersBefore+1),
				zap.Float64("efficiency", efficiency),
				zap.Float64("threshold", threshold),
				zap.Float64("cv", cv),
				zap.Int("queue", queueLen),
				zap.Int32("extraWorkers", extraWorkers))
		}
		return probeSuccess, extraWorkers
	}

	// Failed: apply exponential backoff
	// But reset if CV spiked (workload changed, re-explore)
	if c.lastFailedAt == c.workersBefore+1 {
		// Check for CV spike - workload may have changed
		if c.stableCV > 0 && cv > c.stableCV*cvSpikeThreshold {
			c.failCount = 1 // reset backoff, re-explore
			c.stableCV = cv
		} else {
			c.failCount++
			if c.stableCV == 0 || cv < c.stableCV {
				c.stableCV = cv // track stable CV
			}
		}
	} else {
		c.failCount = 1
		c.lastFailedAt = c.workersBefore + 1
		c.stableCV = cv
	}
	c.setCooldown(now, false)

	if c.log != nil {
		c.log.Info("probe: failed - removing worker",
			zap.Int32("workers", c.workersBefore),
			zap.Float64("efficiency", efficiency),
			zap.Float64("threshold", threshold),
			zap.Float64("cv", cv),
			zap.Int("queue", queueLen),
			zap.Int("backoff", 1<<min(c.failCount, maxBackoffPower)))
	}

	return probeFail, 0
}

// decideScaleDown calculates target worker count (AIMD: multiplicative decrease).
func (c *controller) decideScaleDown(now time.Time, workers, busy int32, util float64) (scaleDecision, int32) {
	// AIMD: multiplicative decrease (50% reduction max)
	target := workers / 2

	// But keep at least busy + 1 (room for new work)
	if target < busy+1 {
		target = busy + 1
	}

	// Respect minimum
	if target < int32(c.MinWorkers) {
		target = int32(c.MinWorkers)
	}

	if target >= workers {
		return scaleNone, 0
	}

	c.cooldownUntil = now.Add(c.cooldown / 2)

	if c.log != nil {
		c.log.Info("scaling down",
			zap.Int32("from", workers),
			zap.Int32("to", target),
			zap.Int32("busy", busy),
			zap.Float64("utilization", util))
	}

	return scaleDown, target
}

// setCooldown applies cooldown with exponential backoff and jitter.
func (c *controller) setCooldown(now time.Time, success bool) {
	var duration time.Duration
	if success {
		// Success: short cooldown for faster scaling
		duration = c.cooldown / 2
	} else {
		// Failure: exponential backoff
		multiplier := 1 << min(c.failCount, maxBackoffPower)
		duration = c.cooldown * time.Duration(multiplier)
	}

	// Add jitter to prevent correlation with workload patterns
	jitter := time.Duration(float64(duration) * jitterFraction * (rand.Float64()*2 - 1))
	c.cooldownUntil = now.Add(duration + jitter)
}
