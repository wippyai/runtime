package pool

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"go.uber.org/zap"
)

// Controller states for the hill-climbing state machine.
// The algorithm probes by adding workers, measures throughput change,
// and keeps the change only if it improves performance.
const (
	stateStable   = iota // Normal operation, monitoring for scaling triggers
	stateProbing         // Testing if adding a worker helps throughput
	stateCooldown        // Waiting after a scaling decision before next action
)

// Default values for adaptive pool configuration.
// Configure using functional options (WithMaxWorkers, WithProbeCooldown, etc.).
// Defaults tuned for responsive scaling with IO-heavy workloads.
const (
	DefaultControlInterval         = 500 * time.Millisecond // Fast sampling for quick response
	DefaultMinElapsed              = 100 * time.Millisecond
	DefaultEMASmoothingFactor      = 0.15 // Lower = smoother, less noise at high scale
	DefaultIdleTicksToScaleDown    = 8    // 8 ticks * 500ms = 4 seconds idle before scale down
	DefaultScaleDownCooldown       = 1 * time.Second
	DefaultProbeCooldown           = 2 * time.Second // Quick probe evaluation
	DefaultProbeFailedCooldown     = 4 * time.Second
	DefaultQueuePressureOverride   = 4 // 4 ticks * 500ms = 2 seconds of sustained pressure
	DefaultQueuePressureRatio      = 0.25
	DefaultImprovementThreshold    = 0.03 // 3% throughput improvement required
	DefaultMinImprovementThreshold = 0.00 // Any positive improvement with queue pressure
	DefaultQueuePressureThreshold  = 0.40 // 40% queue fill to consider pressure
	DefaultLearningInvalidation    = 1.5
	DefaultQueueToWorkerRatio      = 10
	DefaultQueueDropMinBaseline    = 100
	DefaultMaxWorkers              = 16
	DefaultQueueMultiplier         = 64

	// Burst scaling: add multiple workers when improvement is very high
	DefaultBurstThreshold     = 0.15
	DefaultBurstMaxWorkers    = 8
	DefaultSuccessCooldownDiv = 4

	// Aggressive scale-down when many workers are idle
	DefaultAggressiveIdleRatio = 0.5
	DefaultAggressiveKeepRatio = 0.3
)

// Adaptive is a pool that scales workers based on throughput optimization.
//
// The adaptive pool uses hill-climbing optimization with exponential moving average (EMA)
// smoothing. Core principle: fewer workers is better - only add when proven beneficial.
//
// Use NewAdaptive with functional options to configure:
//
//	pool, err := NewAdaptive(factory, dispatcher,
//	    WithMaxWorkers(8),
//	    WithControlInterval(time.Second),
//	    WithImprovementThreshold(0.05),
//	)
type Adaptive struct {
	factory    process.FactoryFunc
	dispatcher dispatcher.Dispatcher
	hooks      ExecutionHooks
	log        *zap.Logger

	// Configuration (set via functional options)
	minWorkers                 int
	maxWorkers                 int
	queueSize                  int
	controlInterval            time.Duration
	emaSmoothingFactor         float64
	idleScaleDownTicks         int
	scaleDownCooldown          time.Duration
	probeCooldown              time.Duration
	probeFailedCooldown        time.Duration
	queuePressureOverrideTicks int
	queuePressureRatio         float64
	improvementThreshold       float64
	minImprovementThreshold    float64
	queuePressureThreshold     float64
	learningInvalidationRatio  float64
	queueToWorkerRatio         int
	queueDropMinBaseline       int
	burstThreshold             float64
	burstMaxWorkers            int
	successCooldownDivisor     int
	aggressiveIdleRatio        float64
	aggressiveKeepRatio        float64

	// Task queue
	tasks   chan *request
	reqPool sync.Pool

	// Worker management
	mu          sync.Mutex
	workers     []*adaptiveWorker
	workerCount atomic.Int32
	busyWorkers atomic.Int32

	// Throughput tracking
	completedOps atomic.Int64

	// Controller state (protected by ctrlMu)
	ctrlMu         sync.Mutex
	state          int
	ema            float64
	baseline       float64
	baselineQueue  int
	lastOps        int64
	lastTime       time.Time
	cooldownUntil  time.Time
	idleTicks      int
	highQueueTicks int

	// Contextual learning
	learnedWorkers    int32
	learnedThroughput float64

	// Scaling state
	consecutiveSuccess int
	recentIdleTicks    int

	// Lifecycle
	done      chan struct{}
	closed    atomic.Bool
	wg        sync.WaitGroup
	startOnce sync.Once

	// Active executions for message routing
	active    sync.Map
	executors sync.Pool
}

// adaptiveWorker processes tasks from the shared queue.
type adaptiveWorker struct {
	pool     *Adaptive
	process  process.Process
	executor *Executor
	stop     chan struct{}
	id       int
}

// AdaptiveOption configures an adaptive pool using the functional options pattern.
type AdaptiveOption func(*Adaptive)

// WithMaxWorkers sets the maximum number of workers the pool can scale to.
// Default: 16
func WithMaxWorkers(n int) AdaptiveOption {
	return func(a *Adaptive) {
		if n > 0 {
			a.maxWorkers = n
		}
	}
}

// WithQueueSize sets the task queue capacity.
// Default: MaxWorkers * 64
func WithQueueSize(n int) AdaptiveOption {
	return func(a *Adaptive) {
		if n > 0 {
			a.queueSize = n
		}
	}
}

// WithControlInterval sets how often the controller evaluates scaling decisions.
// 500ms balances responsiveness with measurement stability.
// Default: 500ms
func WithControlInterval(d time.Duration) AdaptiveOption {
	return func(a *Adaptive) {
		if d > 0 {
			a.controlInterval = d
		}
	}
}

// WithEMASmoothingFactor sets alpha for exponential moving average: EMA = alpha*new + (1-alpha)*old
// 0.3 gives ~86% weight to last 5 samples, balancing noise reduction with responsiveness.
// Weight of sample n ticks ago = alpha * (1-alpha)^n (exponential decay).
// Default: 0.3
func WithEMASmoothingFactor(alpha float64) AdaptiveOption {
	return func(a *Adaptive) {
		if alpha > 0 && alpha < 1 {
			a.emaSmoothingFactor = alpha
		}
	}
}

// WithIdleScaleDownTicks sets how many consecutive idle ticks before removing a worker.
// With default ControlInterval of 500ms, 4 ticks = 2 seconds of idle time.
// Default: 4
func WithIdleScaleDownTicks(n int) AdaptiveOption {
	return func(a *Adaptive) {
		if n > 0 {
			a.idleScaleDownTicks = n
		}
	}
}

// WithScaleDownCooldown sets the cooldown after removing a worker to prevent oscillation.
// Default: 1s
func WithScaleDownCooldown(d time.Duration) AdaptiveOption {
	return func(a *Adaptive) {
		if d > 0 {
			a.scaleDownCooldown = d
		}
	}
}

// WithProbeCooldown sets wait time after adding a worker before measuring improvement.
// Allows system to stabilize and collect meaningful throughput data.
// Default: 2s
func WithProbeCooldown(d time.Duration) AdaptiveOption {
	return func(a *Adaptive) {
		if d > 0 {
			a.probeCooldown = d
		}
	}
}

// WithProbeFailedCooldown sets longer cooldown after a failed probe to avoid thrashing.
// Default: 3s
func WithProbeFailedCooldown(d time.Duration) AdaptiveOption {
	return func(a *Adaptive) {
		if d > 0 {
			a.probeFailedCooldown = d
		}
	}
}

// WithQueuePressureOverrideTicks sets ticks of sustained high queue before forcing a probe,
// even if previous learning suggests we don't need more workers.
// With default ControlInterval of 500ms, 10 ticks = 5 seconds of sustained pressure.
// Default: 10
func WithQueuePressureOverrideTicks(n int) AdaptiveOption {
	return func(a *Adaptive) {
		if n > 0 {
			a.queuePressureOverrideTicks = n
		}
	}
}

// WithQueuePressureRatio sets the queue fill ratio (0.0-1.0) considered "under pressure".
// 0.25 means 25% of queue capacity triggers queue pressure tracking.
// Default: 0.25
func WithQueuePressureRatio(ratio float64) AdaptiveOption {
	return func(a *Adaptive) {
		if ratio > 0 && ratio < 1 {
			a.queuePressureRatio = ratio
		}
	}
}

// WithImprovementThreshold sets the minimum throughput improvement to accept a probe.
// 0.03 means 3% improvement is considered a clear win.
// Default: 0.03
func WithImprovementThreshold(threshold float64) AdaptiveOption {
	return func(a *Adaptive) {
		if threshold >= 0 && threshold < 1 {
			a.improvementThreshold = threshold
		}
	}
}

// WithMinImprovementThreshold sets threshold for accepting improvement with queue pressure.
// When queue is high or dropped significantly, this lower bar is used instead.
// 0.00 means any positive improvement is accepted under pressure.
// Default: 0.00
func WithMinImprovementThreshold(threshold float64) AdaptiveOption {
	return func(a *Adaptive) {
		if threshold >= 0 && threshold < 1 {
			a.minImprovementThreshold = threshold
		}
	}
}

// WithQueuePressureThreshold sets queue fill ratio (0.0-1.0) for accepting marginal improvements.
// When queue > capacity * threshold, marginal improvements are accepted more readily.
// 0.40 means 40% queue fill triggers this behavior.
// Default: 0.40
func WithQueuePressureThreshold(threshold float64) AdaptiveOption {
	return func(a *Adaptive) {
		if threshold > 0 && threshold < 1 {
			a.queuePressureThreshold = threshold
		}
	}
}

// WithLearningInvalidationRatio sets when to invalidate learned optimal workers.
// Invalidates when current throughput exceeds learned throughput by this factor.
// 1.5 means if EMA > learned*1.5, conditions have changed significantly.
// Default: 1.5
func WithLearningInvalidationRatio(ratio float64) AdaptiveOption {
	return func(a *Adaptive) {
		if ratio > 1 {
			a.learningInvalidationRatio = ratio
		}
	}
}

// WithQueueToWorkerRatio sets when to trigger scaling consideration.
// Scaling is considered when queue > workers * ratio.
// 10 means queue > workers*10 suggests workers can't keep up.
// Default: 10
func WithQueueToWorkerRatio(ratio int) AdaptiveOption {
	return func(a *Adaptive) {
		if ratio > 0 {
			a.queueToWorkerRatio = ratio
		}
	}
}

// WithQueueDropMinBaseline sets minimum baseline queue to consider queue drop as success.
// Prevents false positives from small queue fluctuations.
// Default: 100
func WithQueueDropMinBaseline(n int) AdaptiveOption {
	return func(a *Adaptive) {
		if n > 0 {
			a.queueDropMinBaseline = n
		}
	}
}

// WithBurstThreshold sets the improvement threshold to trigger burst scaling.
// When improvement exceeds this, extra workers are added immediately.
// 0.20 means 20% improvement triggers burst scaling.
// Default: 0.20
func WithBurstThreshold(threshold float64) AdaptiveOption {
	return func(a *Adaptive) {
		if threshold > 0 && threshold < 1 {
			a.burstThreshold = threshold
		}
	}
}

// WithBurstMaxWorkers sets maximum workers to add in a single burst.
// Prevents over-scaling from a single successful probe.
// Default: 4
func WithBurstMaxWorkers(n int) AdaptiveOption {
	return func(a *Adaptive) {
		if n > 0 {
			a.burstMaxWorkers = n
		}
	}
}

// WithSuccessCooldownDivisor sets how much to reduce cooldown after successful probe.
// Cooldown is divided by this value after success for faster scaling.
// 2 means cooldown is halved after success.
// Default: 2
func WithSuccessCooldownDivisor(n int) AdaptiveOption {
	return func(a *Adaptive) {
		if n > 0 {
			a.successCooldownDivisor = n
		}
	}
}

// WithAggressiveIdleRatio sets the idle/total worker ratio that triggers aggressive scale-down.
// If idle > workers * ratio, multiple workers are removed at once.
// 0.5 means if more than 50% of workers are idle, scale down aggressively.
// Default: 0.5
func WithAggressiveIdleRatio(ratio float64) AdaptiveOption {
	return func(a *Adaptive) {
		if ratio > 0 && ratio < 1 {
			a.aggressiveIdleRatio = ratio
		}
	}
}

// WithAggressiveKeepRatio sets what fraction of workers to keep during aggressive scale-down.
// 0.3 means keep 30% of current workers (subject to minWorkers floor).
// Default: 0.3
func WithAggressiveKeepRatio(ratio float64) AdaptiveOption {
	return func(a *Adaptive) {
		if ratio > 0 && ratio < 1 {
			a.aggressiveKeepRatio = ratio
		}
	}
}

// WithExecutionHooks sets callbacks for execution lifecycle events.
func WithExecutionHooks(hooks ExecutionHooks) AdaptiveOption {
	return func(a *Adaptive) {
		a.hooks = hooks
	}
}

// WithLogger sets the logger for the adaptive pool.
func WithLogger(log *zap.Logger) AdaptiveOption {
	return func(a *Adaptive) {
		a.log = log
	}
}

// NewAdaptive creates an adaptive pool that scales workers based on throughput.
//
// The pool uses hill-climbing optimization: it probes by adding workers, measures
// throughput change, and keeps the change only if it improves performance.
//
// Configure using functional options:
//
//	pool, err := NewAdaptive(factory, dispatcher,
//	    WithMaxWorkers(8),
//	    WithProbeCooldown(3*time.Second),
//	)
func NewAdaptive(factory process.FactoryFunc, d dispatcher.Dispatcher, opts ...AdaptiveOption) (*Adaptive, error) {
	a := &Adaptive{
		factory:    factory,
		dispatcher: d,

		// Set defaults
		minWorkers:                 1,
		maxWorkers:                 DefaultMaxWorkers,
		controlInterval:            DefaultControlInterval,
		emaSmoothingFactor:         DefaultEMASmoothingFactor,
		idleScaleDownTicks:         DefaultIdleTicksToScaleDown,
		scaleDownCooldown:          DefaultScaleDownCooldown,
		probeCooldown:              DefaultProbeCooldown,
		probeFailedCooldown:        DefaultProbeFailedCooldown,
		queuePressureOverrideTicks: DefaultQueuePressureOverride,
		queuePressureRatio:         DefaultQueuePressureRatio,
		improvementThreshold:       DefaultImprovementThreshold,
		minImprovementThreshold:    DefaultMinImprovementThreshold,
		queuePressureThreshold:     DefaultQueuePressureThreshold,
		learningInvalidationRatio:  DefaultLearningInvalidation,
		queueToWorkerRatio:         DefaultQueueToWorkerRatio,
		queueDropMinBaseline:       DefaultQueueDropMinBaseline,
		burstThreshold:             DefaultBurstThreshold,
		burstMaxWorkers:            DefaultBurstMaxWorkers,
		successCooldownDivisor:     DefaultSuccessCooldownDiv,
		aggressiveIdleRatio:        DefaultAggressiveIdleRatio,
		aggressiveKeepRatio:        DefaultAggressiveKeepRatio,

		done:     make(chan struct{}),
		lastTime: time.Now(),
	}

	// Apply functional options
	for _, opt := range opts {
		opt(a)
	}

	// Set queue size after options (may depend on maxWorkers)
	if a.queueSize <= 0 {
		a.queueSize = a.maxWorkers * DefaultQueueMultiplier
	}

	a.tasks = make(chan *request, a.queueSize)
	a.workers = make([]*adaptiveWorker, 0, a.maxWorkers)

	a.reqPool.New = func() any {
		return &request{resultCh: make(chan *runtime.Result, 1)}
	}

	a.executors.New = func() any {
		return NewExecutor(d).WithExecutionHooks(a.hooks)
	}

	return a, nil
}

// Start launches the pool and begins accepting calls.
func (a *Adaptive) Start() {
	a.startOnce.Do(func() {
		if a.log != nil {
			a.log.Info("starting pool",
				zap.Int("min", a.minWorkers),
				zap.Int("max", a.maxWorkers),
				zap.Int("queue", cap(a.tasks)))
		}

		for i := 0; i < a.minWorkers; i++ {
			if err := a.spawnWorker(); err != nil {
				if a.log != nil {
					a.log.Error("failed to spawn initial worker", zap.Error(err))
				}
				break
			}
		}

		a.wg.Add(1)
		go a.controlLoop()
	})
}

// Stop gracefully shuts down the pool.
func (a *Adaptive) Stop() {
	if a.closed.Swap(true) {
		return
	}
	close(a.done)

	a.mu.Lock()
	for _, w := range a.workers {
		close(w.stop)
	}
	a.mu.Unlock()

	a.wg.Wait()

	a.mu.Lock()
	for _, w := range a.workers {
		w.process.Close()
	}
	a.workers = nil
	a.mu.Unlock()

	if a.log != nil {
		a.log.Info("pool stopped")
	}
}

// EMA returns the current exponential moving average throughput (for testing/debugging).
func (a *Adaptive) EMA() float64 {
	a.ctrlMu.Lock()
	defer a.ctrlMu.Unlock()
	return a.ema
}

// HighQueueTicks returns the current queue pressure tick count (for testing/debugging).
func (a *Adaptive) HighQueueTicks() int {
	a.ctrlMu.Lock()
	defer a.ctrlMu.Unlock()
	return a.highQueueTicks
}

// Send implements relay.Receiver for message routing.
func (a *Adaptive) Send(pkg *relay.Package) error {
	v, ok := a.active.Load(pkg.Target.UniqID)
	if !ok {
		return process.ErrProcessNotFound
	}
	return v.(*Executor).Send(pkg)
}

// Call executes a function call using an available worker.
func (a *Adaptive) Call(ctx context.Context, method string, input payload.Payloads) (*runtime.Result, error) {
	if a.closed.Load() {
		return nil, ErrPoolClosed
	}

	req := a.reqPool.Get().(*request)
	req.ctx = ctx
	req.method = method
	req.input = input

	select {
	case a.tasks <- req:
	default:
		select {
		case a.tasks <- req:
		case <-ctx.Done():
			a.reqPool.Put(req)
			return nil, ctx.Err()
		case <-a.done:
			a.reqPool.Put(req)
			return nil, ErrPoolClosed
		}
	}

	result := <-req.resultCh
	a.reqPool.Put(req)
	return result, nil
}

func (a *Adaptive) spawnWorker() error {
	a.mu.Lock()
	if len(a.workers) >= a.maxWorkers {
		a.mu.Unlock()
		return nil
	}

	proc, err := a.factory()
	if err != nil {
		a.mu.Unlock()
		return err
	}

	w := &adaptiveWorker{
		pool:     a,
		process:  proc,
		executor: a.executors.Get().(*Executor),
		stop:     make(chan struct{}),
		id:       len(a.workers),
	}
	a.workers = append(a.workers, w)
	a.workerCount.Store(int32(len(a.workers)))
	a.mu.Unlock()

	a.wg.Add(1)
	go w.run()

	return nil
}

func (a *Adaptive) removeWorker() bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.workers) <= a.minWorkers {
		return false
	}

	idx := len(a.workers) - 1
	w := a.workers[idx]
	a.workers = a.workers[:idx]
	a.workerCount.Store(int32(len(a.workers)))

	close(w.stop)
	return true
}

func (a *Adaptive) controlLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(a.controlInterval)
	defer ticker.Stop()

	for {
		select {
		case <-a.done:
			return
		case <-ticker.C:
			a.control()
		}
	}
}

func (a *Adaptive) control() {
	now := time.Now()
	workers := a.workerCount.Load()
	busy := a.busyWorkers.Load()
	// Clamp to avoid negative idle during worker removal race
	if busy > workers {
		busy = workers
	}
	idle := workers - busy
	queueLen := len(a.tasks)

	ops := a.completedOps.Load()

	a.ctrlMu.Lock()
	defer a.ctrlMu.Unlock()

	elapsed := now.Sub(a.lastTime).Seconds()
	minElapsed := float64(DefaultMinElapsed) / float64(time.Second)
	if elapsed < minElapsed {
		return
	}

	throughput := float64(ops-a.lastOps) / elapsed
	a.lastOps = ops
	a.lastTime = now

	if a.ema == 0 {
		a.ema = throughput
	} else {
		a.ema = a.emaSmoothingFactor*throughput + (1-a.emaSmoothingFactor)*a.ema
	}

	// Track idle workers to detect bursty workloads
	// If we see idle workers, we probably don't need more workers
	if idle > 0 {
		a.recentIdleTicks++
		if a.recentIdleTicks > 10 {
			a.recentIdleTicks = 10 // Cap at 10
		}
	} else {
		if a.recentIdleTicks > 0 {
			a.recentIdleTicks--
		}
	}

	// Track queue pressure even during cooldown
	// Detect pressure by either:
	// 1. Queue fill percentage > threshold (for small queues)
	// 2. All workers busy with any queue AND no recent idle (not just bursty)
	queueCap := cap(a.tasks)
	queuePct := float64(queueLen) / float64(queueCap)
	sustainedPressure := idle == 0 && queueLen > 0 && a.recentIdleTicks == 0
	if queuePct > a.queuePressureRatio || sustainedPressure {
		a.highQueueTicks++
	} else {
		a.highQueueTicks = 0
	}

	inCooldown := now.Before(a.cooldownUntil)
	if inCooldown {
		return
	}

	switch a.state {
	case stateStable:
		a.handleStable(workers, idle, queueLen)
	case stateProbing:
		a.handleProbing(workers)
	case stateCooldown:
		a.state = stateStable
	}
}

func (a *Adaptive) handleStable(workers, idle int32, queueLen int) {
	queuePct := float64(queueLen) / float64(cap(a.tasks))

	if queueLen == 0 && idle > 0 && workers > int32(a.minWorkers) {
		a.idleTicks++
		a.highQueueTicks = 0
		if a.idleTicks >= a.idleScaleDownTicks {
			idleRatio := float64(idle) / float64(workers)

			// Aggressive scale-down: if many workers idle, remove multiple at once
			if idleRatio > a.aggressiveIdleRatio {
				// Keep only a fraction of current workers (but respect minWorkers)
				targetWorkers := int32(float64(workers) * a.aggressiveKeepRatio)
				if targetWorkers < int32(a.minWorkers) {
					targetWorkers = int32(a.minWorkers)
				}
				toRemove := int(workers - targetWorkers)
				if toRemove > 0 {
					removed := 0
					for i := 0; i < toRemove; i++ {
						if a.removeWorker() {
							removed++
						} else {
							break
						}
					}
					if a.log != nil {
						a.log.Debug("aggressive scale down",
							zap.Int("removed", removed),
							zap.Float64("idleRatio", idleRatio),
							zap.Int32("workers", a.workerCount.Load()))
					}
				}
			} else {
				a.removeWorker()
				if a.log != nil {
					a.log.Debug("scale down", zap.Int32("workers", a.workerCount.Load()))
				}
			}

			a.idleTicks = 0
			a.cooldownUntil = time.Now().Add(a.scaleDownCooldown)
		}
		return
	}
	a.idleTicks = 0

	// Trigger scale-up probe when sustained pressure (not just bursty queue)
	// Don't probe if we've seen idle workers recently - that means we have enough
	if idle == 0 && queueLen > 0 && workers < int32(a.maxWorkers) && a.recentIdleTicks == 0 {
		learningValid := a.learnedWorkers > 0 &&
			a.learnedThroughput > 0 &&
			a.ema < a.learnedThroughput*a.learningInvalidationRatio

		// Override learning if:
		// 1. Queue pressure sustained (qTicks >= threshold)
		// 2. Queue nearly saturated (>90%)
		// 3. Significant backlog with all workers busy (requests waiting > workers/2)
		significantBacklog := queueLen > int(workers)/2
		if a.highQueueTicks >= a.queuePressureOverrideTicks || queuePct > 0.90 || significantBacklog {
			learningValid = false
			a.highQueueTicks = 0
		}

		if learningValid && workers >= a.learnedWorkers {
			return
		}

		// At high scale (>100 workers), skip probing - just add workers
		// Probing is useless when measurement noise exceeds 1/workers signal
		if workers > 100 && significantBacklog {
			// Add workers proportional to backlog
			toAdd := queueLen / int(workers)
			if toAdd < 1 {
				toAdd = 1
			}
			if toAdd > a.burstMaxWorkers {
				toAdd = a.burstMaxWorkers
			}
			added := 0
			for i := 0; i < toAdd; i++ {
				if err := a.spawnWorker(); err != nil {
					break
				}
				added++
			}
			a.learnedWorkers = a.workerCount.Load()
			a.cooldownUntil = time.Now().Add(a.scaleDownCooldown)
			if a.log != nil {
				a.log.Debug("high-scale add",
					zap.Int("added", added),
					zap.Int("backlog", queueLen),
					zap.Int32("workers", a.workerCount.Load()))
			}
			return
		}

		a.baseline = a.ema
		a.baselineQueue = queueLen
		a.state = stateProbing
		a.spawnWorker()
		a.cooldownUntil = time.Now().Add(a.probeCooldown)
		if a.log != nil {
			a.log.Debug("probing up",
				zap.Int32("workers", a.workerCount.Load()),
				zap.Float64("baseline", a.baseline),
				zap.Int("queue", a.baselineQueue))
		}
	}
}

func (a *Adaptive) handleProbing(workers int32) {
	improvement := (a.ema - a.baseline) / a.baseline
	queueLen := len(a.tasks)
	queuePct := float64(queueLen) / float64(cap(a.tasks))
	busy := a.busyWorkers.Load()
	allBusy := busy >= workers
	hasBacklog := queueLen > 0
	queueDropped := a.baselineQueue > a.queueDropMinBaseline && queueLen < a.baselineQueue/2

	// Calculate per-worker efficiency: how much of the theoretical max improvement did we get?
	// Theoretical max = 1/workers (adding 1 worker to N workers can at best improve by 1/N)
	theoreticalMax := 1.0 / float64(workers-1) // workers before this probe
	efficiency := improvement / theoreticalMax // 1.0 = perfect linear scaling

	// At high worker counts, throughput variance exceeds signal from +1 worker
	// Use queue behavior as alternative success signal
	queueImproved := a.baselineQueue > 0 && queueLen < a.baselineQueue
	noRegression := improvement > -theoreticalMax // Not worse than losing one worker's worth

	// Success conditions:
	// 1. Clear throughput improvement (>improvementThreshold)
	// 2. Any improvement with current queue pressure (high fill %)
	// 3. Any positive improvement when all busy with backlog (worker is being utilized)
	// 4. Throughput stable/improved AND queue dropped significantly
	// 5. Good per-worker efficiency (>50% of theoretical max) when busy
	// 6. At high scale: queue improved AND no significant regression (noise-tolerant)
	highScale := workers > 100
	success := improvement > a.improvementThreshold ||
		(improvement > a.minImprovementThreshold && queuePct > a.queuePressureThreshold) ||
		(improvement > a.minImprovementThreshold && allBusy && hasBacklog) ||
		(improvement > a.minImprovementThreshold && queueDropped) ||
		(efficiency > 0.5 && allBusy) ||
		(highScale && queueImproved && noRegression && allBusy) // queue going down = worker helping

	if success {
		a.state = stateStable
		a.learnedWorkers = workers
		a.learnedThroughput = a.ema
		a.consecutiveSuccess++

		if hasBacklog && workers < int32(a.maxWorkers) && a.consecutiveSuccess >= 2 {
			predictedJump := 1 << min(a.consecutiveSuccess, 4)
			available := a.maxWorkers - int(workers)
			if predictedJump > available {
				predictedJump = available
			}
			if predictedJump > a.burstMaxWorkers*2 {
				predictedJump = a.burstMaxWorkers * 2
			}

			for i := 0; i < predictedJump; i++ {
				if err := a.spawnWorker(); err != nil {
					break
				}
			}
			if predictedJump > 0 {
				a.learnedWorkers = a.workerCount.Load()
			}
		}

		a.cooldownUntil = time.Now().Add(a.probeCooldown / time.Duration(a.successCooldownDivisor))
	} else {
		a.removeWorker()
		a.learnedWorkers = workers - 1
		a.learnedThroughput = a.ema
		a.state = stateStable
		a.cooldownUntil = time.Now().Add(a.probeFailedCooldown)
		a.highQueueTicks = 0
		a.consecutiveSuccess = 0
	}
}

func (w *adaptiveWorker) run() {
	defer w.pool.wg.Done()
	defer func() {
		w.process.Close()
		w.pool.executors.Put(w.executor)
	}()

	for {
		select {
		case <-w.pool.done:
			w.drain()
			return
		case <-w.stop:
			w.drain()
			return
		case req := <-w.pool.tasks:
			w.execute(req)
		}
	}
}

func (w *adaptiveWorker) drain() {
	for {
		select {
		case req := <-w.pool.tasks:
			w.execute(req)
		default:
			return
		}
	}
}

func (w *adaptiveWorker) execute(req *request) {
	w.pool.busyWorkers.Add(1)

	pid, _ := runtime.GetFramePID(req.ctx)
	w.pool.active.Store(pid.UniqID, w.executor)

	result := w.executor.Run(req.ctx, w.process, req.method, req.input)

	w.pool.active.Delete(pid.UniqID)
	w.pool.busyWorkers.Add(-1)
	w.pool.completedOps.Add(1)

	req.resultCh <- result
}
