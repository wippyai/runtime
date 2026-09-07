// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	relayapi "github.com/wippyai/runtime/api/relay"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	actorhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
)

// --- Open-Loop Load Driver Types & Accounting ---

type openLoopOpType int

const (
	opGet openLoopOpType = iota
	opPut
)

type openLoopRequest struct {
	scheduledAt time.Time
	id          uint64
	actorIdx    int
	op          openLoopOpType
}

type openLoopConfig struct {
	mailboxLimits    *actorhost.Limits
	actors           int
	rowsPerActor     int64
	workers          int
	queueCapacity    int
	maxPending       int     // explicit per-worker/per-actor pending cap (default 64)
	targetRate       float64 // arrivals per second
	duration         time.Duration
	reqTimeout       time.Duration
	drainBudget      time.Duration
	writeRatio       int // percentage of PUT operations (0..100)
	gateHoldDuration time.Duration
	gated            bool
}

func validateConfig(cfg openLoopConfig) {
	if cfg.workers < 1 {
		panic("workers must be >= 1")
	}
	if cfg.actors < 1 {
		panic("actors must be >= 1")
	}
	if cfg.rowsPerActor < int64(cfg.workers) {
		panic(fmt.Sprintf("rowsPerActor (%d) must be >= workers (%d)", cfg.rowsPerActor, cfg.workers))
	}
	if cfg.targetRate <= 0 {
		panic("targetRate must be > 0")
	}
	if cfg.duration <= 0 {
		panic("duration must be > 0")
	}
	if cfg.maxPending < 0 {
		panic("maxPending must be >= 0")
	}
}

type openLoopCounts struct {
	planned          atomic.Uint64
	offered          atomic.Uint64
	generatorDropped atomic.Uint64
	admitted         atomic.Uint64
	failedAdmission  atomic.Uint64
	completed        atomic.Uint64
	failedExecution  atomic.Uint64
	failed           atomic.Uint64
	timedout         atomic.Uint64
}

func (c *openLoopCounts) assertConservation(t testing.TB) {
	t.Helper()
	p := c.planned.Load()
	off := c.offered.Load()
	gd := c.generatorDropped.Load()
	adm := c.admitted.Load()
	fa := c.failedAdmission.Load()
	comp := c.completed.Load()
	fe := c.failedExecution.Load()
	fail := c.failed.Load()
	to := c.timedout.Load()

	require.Equal(t, p, off+gd, "planned == offered + generator_dropped")
	require.Equal(t, off, adm+fa, "offered == admitted + failed_admission")
	require.Equal(t, adm, comp+fe+to, "admitted == completed + failed_execution + timedout")
	require.Equal(t, fail, fa+fe, "failed == failed_admission + failed_execution")
	require.Equal(t, off, comp+fail+to, "offered == completed + failed + timedout")
	require.Equal(t, p, comp+fail+to+gd, "planned == completed + failed + timedout + generator_dropped")
}

// --- Memory Time Series ---

type memorySample struct {
	elapsed   time.Duration
	heapAlloc uint64
	heapInuse uint64
	sys       uint64
	numGC     uint32
}

type memoryTimeSeries struct {
	samples    []memorySample
	mu         sync.Mutex
	maxSamples int
	peakHeap   uint64
}

func newMemoryTimeSeries(maxSamples int) *memoryTimeSeries {
	return &memoryTimeSeries{
		samples:    make([]memorySample, 0, maxSamples),
		maxSamples: maxSamples,
	}
}

func (m *memoryTimeSeries) sample(start time.Time) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	m.mu.Lock()
	defer m.mu.Unlock()

	if ms.HeapAlloc > m.peakHeap {
		m.peakHeap = ms.HeapAlloc
	}

	if len(m.samples) < m.maxSamples {
		m.samples = append(m.samples, memorySample{
			elapsed:   time.Since(start),
			heapAlloc: ms.HeapAlloc,
			heapInuse: ms.HeapInuse,
			sys:       ms.Sys,
			numGC:     ms.NumGC,
		})
	}
}

func (m *memoryTimeSeries) Slope() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.samples)
	if n < 2 {
		return 0
	}
	first := m.samples[0]
	last := m.samples[n-1]
	dt := last.elapsed.Seconds() - first.elapsed.Seconds()
	if dt <= 0 {
		return 0
	}
	dAlloc := float64(int64(last.heapAlloc) - int64(first.heapAlloc))
	return dAlloc / dt
}

func (m *memoryTimeSeries) PeakHeap() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.peakHeap
}

// --- Sound Deterministic Partitioned Reference Oracle ---

const maxRecordedSemanticErrors = 64

type pendingOp struct {
	scheduledAt time.Time
	reqID       uint64
	actorIdx    int
	op          openLoopOpType
	rowID       int64
	sentVal     int64
	prevVal     int64
	writeSeq    uint64
}

type pendingQueue struct {
	ops   []*pendingOp
	head  int
	count int
}

func newPendingQueue(capacity int) *pendingQueue {
	if capacity <= 0 {
		panic("pending queue capacity must be positive")
	}
	return &pendingQueue{ops: make([]*pendingOp, capacity)}
}

func (q *pendingQueue) isFull() bool { return q.count == len(q.ops) }

func (q *pendingQueue) push(op *pendingOp) bool {
	if q.isFull() {
		return false
	}
	q.ops[(q.head+q.count)%len(q.ops)] = op
	q.count++
	return true
}

func (q *pendingQueue) pop() *pendingOp {
	if q.count == 0 {
		return nil
	}
	op := q.ops[q.head]
	q.ops[q.head] = nil
	q.head = (q.head + 1) % len(q.ops)
	q.count--
	return op
}

func (q *pendingQueue) len() int      { return q.count }
func (q *pendingQueue) capacity() int { return len(q.ops) }
func (q *pendingQueue) clear() {
	clear(q.ops)
	q.ops = nil
	q.head, q.count = 0, 0
}

type partitionActorState struct {
	acked     map[int64]int64
	ackedSeq  map[int64]uint64
	writeSeq  map[int64]uint64
	uncertain map[int64]map[int64]struct{} // rowID -> set of candidate values
	mu        sync.Mutex
}

func newPartitionActorState() *partitionActorState {
	return &partitionActorState{
		acked:     make(map[int64]int64),
		ackedSeq:  make(map[int64]uint64),
		writeSeq:  make(map[int64]uint64),
		uncertain: make(map[int64]map[int64]struct{}),
	}
}

func (s *partitionActorState) nextWriteSeq(rowID int64) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeSeq[rowID]++
	return s.writeSeq[rowID]
}

func (s *partitionActorState) addUncertain(rowID, prevVal, newVal int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cand, exists := s.uncertain[rowID]
	if !exists {
		cand = make(map[int64]struct{})
		cand[prevVal] = struct{}{}
		s.uncertain[rowID] = cand
	}
	cand[newVal] = struct{}{}
}

func (s *partitionActorState) isValidValue(rowID, val int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cand, exists := s.uncertain[rowID]; exists {
		_, ok := cand[val]
		return ok
	}
	if lastVal, ok := s.acked[rowID]; ok {
		return val == lastVal
	}
	return val == rowID*3
}

func (s *partitionActorState) isUncertain(rowID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, exists := s.uncertain[rowID]
	return exists
}

func (s *partitionActorState) applyAck(rowID, val int64) {
	s.applyAckWithSeq(rowID, val, 0)
}

func (s *partitionActorState) applyAckWithSeq(rowID, val int64, seq uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lastSeq := s.ackedSeq[rowID]
	if seq > 0 && lastSeq > 0 && seq < lastSeq {
		// Late ACK for an older write that has been superseded by a newer acknowledged write.
		// Never overwrite newer acknowledged state!
		return
	}
	s.acked[rowID] = val
	if seq > lastSeq {
		s.ackedSeq[rowID] = seq
	}
	if seq == 0 || seq >= s.writeSeq[rowID] {
		delete(s.uncertain, rowID)
	} else if cand, exists := s.uncertain[rowID]; exists {
		cand[val] = struct{}{}
	}
}

func (s *partitionActorState) getBaseVal(rowID int64) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lastVal, ok := s.acked[rowID]; ok {
		return lastVal
	}
	return rowID * 3
}

type replyOutcome int

const (
	replyMatch replyOutcome = iota
	replyLate
	replySemanticError
)

func (s *partitionActorState) handleReply(reply *relayapi.Message, op *pendingOp) (replyOutcome, error) {
	if reply == nil {
		return replySemanticError, fmt.Errorf("nil reply message")
	}
	if op == nil {
		return replySemanticError, fmt.Errorf("unsolicited reply: no correlated pending operation")
	}
	if reply.Topic != "result" {
		return replySemanticError, fmt.Errorf("unexpected reply topic %q", reply.Topic)
	}
	text, err := parseResultText(reply)
	if err != nil {
		return replySemanticError, fmt.Errorf("malformed reply text: %w", err)
	}

	if strings.HasPrefix(text, "updated:") {
		if op.op != opPut {
			return replySemanticError, fmt.Errorf("received PUT reply for GET request (reqID=%d row=%d)", op.reqID, op.rowID)
		}
		upID, upVal, err := parsePutReply(reply)
		if err != nil {
			return replySemanticError, fmt.Errorf("malformed put reply: %w", err)
		}
		if upID != op.rowID {
			return replySemanticError, fmt.Errorf("put reply row ID mismatch: got %d, expected %d", upID, op.rowID)
		}
		if upVal != op.sentVal {
			return replySemanticError, fmt.Errorf("put reply value mismatch for row %d: got %d, expected sent value %d", upID, upVal, op.sentVal)
		}
		s.applyAckWithSeq(upID, upVal, op.writeSeq)
		return replyMatch, nil
	}

	if strings.HasPrefix(text, "value:") {
		if op.op != opGet {
			return replySemanticError, fmt.Errorf("received GET reply for PUT request (reqID=%d row=%d)", op.reqID, op.rowID)
		}
		gotID, gotVal, err := parseGetReply(reply)
		if err != nil {
			return replySemanticError, fmt.Errorf("malformed get reply: %w", err)
		}
		if gotID != op.rowID {
			return replySemanticError, fmt.Errorf("get reply row ID mismatch: got %d, expected %d", gotID, op.rowID)
		}
		if !s.isValidValue(gotID, gotVal) {
			return replySemanticError, fmt.Errorf("semantic mismatch for row %d: got %d not in legal reference state", gotID, gotVal)
		}
		return replyMatch, nil
	}

	return replySemanticError, fmt.Errorf("unexpected result format: %s", text)
}

type workerPartitionState struct {
	actors []*partitionActorState
}

func newWorkerPartitionState(numActors int) *workerPartitionState {
	actors := make([]*partitionActorState, numActors)
	for i := range actors {
		actors[i] = newPartitionActorState()
	}
	return &workerPartitionState{actors: actors}
}

// --- Open-Loop Load Engine Execution ---

type openLoopResult struct {
	endToEndLatency   *latencyReservoir
	generatorLateness *latencyReservoir
	memSeries         *memoryTimeSeries
	semanticErrors    []error
	counts            openLoopCounts
	duration          time.Duration
	memBaseline       runtime.MemStats
	memEnd            runtime.MemStats
	memCleanup        runtime.MemStats
	semanticDropped   atomic.Uint64
	semanticMu        sync.Mutex
}

func (r *openLoopResult) recordSemanticError(err error) {
	r.semanticMu.Lock()
	defer r.semanticMu.Unlock()
	if len(r.semanticErrors) < maxRecordedSemanticErrors {
		r.semanticErrors = append(r.semanticErrors, err)
	} else {
		r.semanticDropped.Add(1)
	}
}

type workerReplyOutcome uint8

const (
	workerReplyApplied workerReplyOutcome = iota
	workerReplySemanticError
	workerReplyUnsolicited
)

// consumeWorkerReply removes one reply and its correlated pending operation.
// It deliberately reports an empty pending queue rather than silently accepting
// an unsolicited reply.
func consumeWorkerReply(
	reply *relayapi.Message,
	queue *pendingQueue,
	actState *partitionActorState,
	workerID int,
	actorIdx int,
) (workerReplyOutcome, error) {
	headOp := queue.pop()
	if headOp == nil {
		return workerReplyUnsolicited, fmt.Errorf("worker %d: unsolicited reply on actor %d: queue is empty", workerID, actorIdx)
	}
	outcome, err := actState.handleReply(reply, headOp)
	if outcome == replySemanticError {
		return workerReplySemanticError, err
	}
	return workerReplyApplied, nil
}

func recordWorkerReplyFailure(res *openLoopResult, err error) {
	res.counts.failedExecution.Add(1)
	res.counts.failed.Add(1)
	res.recordSemanticError(err)
}

func reportWorkerReplyFailure(t testing.TB, outcome workerReplyOutcome, err error, workerID, actorIdx int) {
	if outcome == workerReplySemanticError {
		t.Errorf("worker %d: semantic failure on actor %d: %v", workerID, actorIdx, err)
		return
	}
	t.Errorf("%v", err)
}

// drainWorkerReplies consumes already available replies for a client. Semantic
// failures remain visible to the test, and an unsolicited reply is never dropped
// without accounting for it.
func drainWorkerReplies(
	client *harnessWorkerClient,
	queue *pendingQueue,
	actState *partitionActorState,
	res *openLoopResult,
	workerID int,
	actorIdx int,
	t testing.TB,
) bool {
	processed := false
	for {
		select {
		case reply := <-client.replyCh:
			outcome, err := consumeWorkerReply(reply, queue, actState, workerID, actorIdx)
			if outcome != workerReplyApplied {
				recordWorkerReplyFailure(res, err)
				reportWorkerReplyFailure(t, outcome, err, workerID, actorIdx)
				if outcome == workerReplyUnsolicited {
					return processed
				}
			}
			processed = true
		default:
			return processed
		}
	}
}

func acceptedReplyDrainDeadlineError(queue *pendingQueue, workerID, actorIdx int) error {
	return fmt.Errorf("accepted reply drain exceeded shared budget with %d pending replies for worker %d actor %d", queue.len(), workerID, actorIdx)
}

// drainAcceptedWorkerReplies waits until every accepted operation remaining in
// one worker/actor pending queue has a correlated reply. All callers share one
// absolute deadline so a slow client cannot receive an additional drain budget.
func drainAcceptedWorkerReplies(
	client *harnessWorkerClient,
	queue *pendingQueue,
	actState *partitionActorState,
	res *openLoopResult,
	workerID int,
	actorIdx int,
	deadline time.Time,
	t testing.TB,
) error {
	// Check before creating a timer or reading replyCh. An expired shared
	// deadline preserves both the pending operation and a buffered reply.
	if queue.len() > 0 && !time.Now().Before(deadline) {
		return acceptedReplyDrainDeadlineError(queue, workerID, actorIdx)
	}
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()

	for queue.len() > 0 {
		if !time.Now().Before(deadline) {
			return acceptedReplyDrainDeadlineError(queue, workerID, actorIdx)
		}
		select {
		case reply := <-client.replyCh:
			// A reply can become selectable concurrently with timer expiry.
			// Retain the correlated pending operation by returning before
			// consumeWorkerReply. The harness receiver forwards a message pointer
			// from its package and ordinary reply consumers do not pool-release it,
			// so this terminal path explicitly discards its received reference and
			// leaves reclamation to normal GC. Buffered reply evidence is guaranteed
			// only on the pre-read expiry path.
			if !time.Now().Before(deadline) {
				_ = reply
				return acceptedReplyDrainDeadlineError(queue, workerID, actorIdx)
			}
			outcome, err := consumeWorkerReply(reply, queue, actState, workerID, actorIdx)
			if outcome != workerReplyApplied {
				recordWorkerReplyFailure(res, err)
				reportWorkerReplyFailure(t, outcome, err, workerID, actorIdx)
			}
		case <-timer.C:
			return acceptedReplyDrainDeadlineError(queue, workerID, actorIdx)
		}
	}
	return nil
}

// waitOrCancelWorkers waits for workers to exit on drainDone within drainBudget.
// If the drain budget expires, cancelWorkers is called to signal all cancellation-aware workers,
// and it waits on the same drainDone channel until all workers join.
// Returns an error if the drain budget was exceeded.
func waitOrCancelWorkers(drainDone <-chan struct{}, cancelWorkers context.CancelFunc, drainBudget time.Duration) error {
	select {
	case <-drainDone:
		return nil
	case <-time.After(drainBudget):
		cancelWorkers()
		<-drainDone
		return fmt.Errorf("worker drain exceeded bounded budget of %v; workers canceled and joined", drainBudget)
	}
}

func runOpenLoopLoad(t testing.TB, cfg openLoopConfig) *openLoopResult {
	t.Helper()

	validateConfig(cfg)

	if cfg.reqTimeout <= 0 {
		cfg.reqTimeout = 2 * time.Second
	}
	if cfg.drainBudget <= 0 {
		cfg.drainBudget = 5 * time.Second
	}
	if cfg.maxPending <= 0 {
		cfg.maxPending = 64
	}

	wasmBytes, ok := loadTestWASMBytes(t, "testdata/sqlite_actor.wasm")
	if !ok {
		t.Fatal("required SQLite fixture testdata/sqlite_actor.wasm is unavailable")
	}

	mailboxLimits := actorhost.DefaultLimits()
	if cfg.mailboxLimits != nil {
		mailboxLimits = *cfg.mailboxLimits
	}

	var activeGated *gatedProcess
	var activeGatedMu sync.Mutex

	factoryFunc := func() (processapi.Process, error) {
		p, err := createWASMActorProcess(context.Background(), wasmBytes, 0, mailboxLimits)
		if err != nil {
			return nil, err
		}
		if cfg.gated {
			gp := newGatedProcess(p)
			activeGatedMu.Lock()
			activeGated = gp
			activeGatedMu.Unlock()
			return gp, nil
		}
		return p, nil
	}

	cluster := newHarnessCluster(t, cfg.workers, factoryFunc)

	type actorHandle struct {
		doneCh chan *runtimeapi.Result
		pid    pid.PID
	}

	actors := make([]*actorHandle, cfg.actors)
	for i := 0; i < cfg.actors; i++ {
		actorPID := cluster.SpawnActor(t, fmt.Sprintf("openloop-actor-%d", i))
		doneCh := cluster.lifecycle.registerWait(actorPID)
		actors[i] = &actorHandle{
			pid:    actorPID,
			doneCh: doneCh,
		}
	}

	// 1. Initial pre-load and baseline oracle verification
	initClient := cluster.NewClient("init-client")
	for i, a := range actors {
		initCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		reply, dur, err := initClient.Request(initCtx, a.pid, fmt.Sprintf("load:%d", cfg.rowsPerActor))
		cancel()
		require.NoError(t, err)
		loadedN, err := parseLoadReply(reply)
		require.NoError(t, err)
		require.Equal(t, cfg.rowsPerActor, loadedN)

		// Verify initial sum
		expectedInitialSum := int64(3) * (cfg.rowsPerActor - 1) * cfg.rowsPerActor / 2
		sumCtx, sumCancel := context.WithTimeout(context.Background(), 10*time.Second)
		sumReply, _, err := initClient.Request(sumCtx, a.pid, "sum")
		sumCancel()
		require.NoError(t, err)
		count, sumVal, err := parseSumReply(sumReply)
		require.NoError(t, err)
		require.Equal(t, cfg.rowsPerActor, count)
		require.Equal(t, expectedInitialSum, sumVal)
		t.Logf("Actor %d loaded %d rows in %v (initial sum oracle verified: %d)", i, loadedN, dur, sumVal)
	}
	initClient.Close()

	// 2. Baseline memory snapshot
	runtime.GC()
	var mBaseline runtime.MemStats
	runtime.ReadMemStats(&mBaseline)

	res := &openLoopResult{
		endToEndLatency:   newLatencyReservoir(20000, 10001),
		generatorLateness: newLatencyReservoir(20000, 20002),
		memSeries:         newMemoryTimeSeries(1000),
		memBaseline:       mBaseline,
	}

	// 3. Worker partitions setup
	workerStates := make([]*workerPartitionState, cfg.workers)
	for w := 0; w < cfg.workers; w++ {
		workerStates[w] = newWorkerPartitionState(cfg.actors)
	}

	// 4. Bounded work queue
	workQueue := make(chan *openLoopRequest, cfg.queueCapacity)

	var stepGate chan struct{}
	var stepGateClosed atomic.Bool
	var gatingClient *harnessWorkerClient
	if cfg.gated {
		activeGatedMu.Lock()
		gp := activeGated
		activeGatedMu.Unlock()
		require.NotNil(t, gp, "gated process must be captured")

		stepGate = make(chan struct{})
		enterStep := make(chan struct{}, 1)
		gp.setGate(stepGate, enterStep)

		gatingClient = cluster.NewClient("gating-client")
		require.NoError(t, gatingClient.SendOnly(actors[0].pid, "get:0"))
		select {
		case <-enterStep:
			t.Log("Scheduler worker deterministically gated inside guest Step")
		case <-time.After(5 * time.Second):
			t.Fatal("Worker failed to enter gated Step")
		}

		if cfg.gateHoldDuration > 0 {
			time.AfterFunc(cfg.gateHoldDuration, func() {
				if stepGateClosed.CompareAndSwap(false, true) {
					close(stepGate)
				}
			})
		}
	}

	// 5. Background memory sampler outside measured request hot path
	stopMemMonitor := make(chan struct{})
	memMonitorDone := make(chan struct{})
	stopProfile := startOpenLoopProfile(t)
	defer stopProfile()
	diagnosticTrace := trace.IsEnabled()
	var slowTraceCount atomic.Uint64
	var workerStatsBefore []map[string]uint64
	if diagnosticTrace {
		workerStatsBefore = cluster.scheduler.WorkerStats()
	}
	startLoad := time.Now()

	go func() {
		defer close(memMonitorDone)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				res.memSeries.sample(startLoad)
			case <-stopMemMonitor:
				return
			}
		}
	}()

	// 6. Spawn fixed worker pool with dedicated clients and ordered pending queues per actor
	workerClients := make([][]*harnessWorkerClient, cfg.workers)
	for wID := 0; wID < cfg.workers; wID++ {
		workerClients[wID] = make([]*harnessWorkerClient, cfg.actors)
		for aIdx := 0; aIdx < cfg.actors; aIdx++ {
			workerClients[wID][aIdx] = cluster.NewClient(fmt.Sprintf("openloop-w-%d-a-%d", wID, aIdx))
		}
	}
	defer func() {
		for _, row := range workerClients {
			for _, c := range row {
				c.Close()
			}
		}
	}()

	workerQueues := make([][]*pendingQueue, cfg.workers)
	for w := 0; w < cfg.workers; w++ {
		workerQueues[w] = make([]*pendingQueue, cfg.actors)
		for a := 0; a < cfg.actors; a++ {
			workerQueues[w][a] = newPendingQueue(cfg.maxPending)
		}
	}

	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()

	var workerWg sync.WaitGroup
	for wID := 0; wID < cfg.workers; wID++ {
		workerWg.Add(1)
		go func(workerID int) {
			defer workerWg.Done()

			clientPool := workerClients[workerID]
			queuePool := workerQueues[workerID]
			rng := rand.New(rand.NewPCG(uint64(workerID+1)*4241, 991))
			numPartitionRows := (cfg.rowsPerActor - int64(workerID) + int64(cfg.workers) - 1) / int64(cfg.workers)
			wState := workerStates[workerID]

		workerLoop:
			for {
				var req *openLoopRequest
				select {
				case <-workerCtx.Done():
					return
				case r, ok := <-workQueue:
					if !ok {
						return
					}
					req = r
				}

				targetActor := actors[req.actorIdx]
				actState := wState.actors[req.actorIdx]
				client := clientPool[req.actorIdx]
				queue := queuePool[req.actorIdx]

				// End-to-end deadline bounded from scheduled arrival time
				deadline := req.scheduledAt.Add(cfg.reqTimeout)
				now := time.Now()
				if !deadline.After(now) {
					// Timed out in queue before worker dequeue: not admitted
					res.counts.failedAdmission.Add(1)
					res.counts.failed.Add(1)
					res.endToEndLatency.Add(now.Sub(req.scheduledAt))
					continue
				}

				reqCtx, reqCancel := context.WithDeadline(workerCtx, deadline)

				// Step 1: Drain any readily available replies for this actor to make progress
				// and clear pending slots before evaluating admission.
				drainWorkerReplies(client, queue, actState, res, workerID, req.actorIdx, t)

				// Step 2: Enforce per-worker/per-actor pending cap backpressure BEFORE admitting.
				// If queue is at capacity, wait for late replies to arrive on client.replyCh
				// until capacity is available, or until this request's deadline expires.
				for queue.isFull() {
					select {
					case reply := <-client.replyCh:
						headOp := queue.pop()
						if headOp == nil {
							res.counts.failedExecution.Add(1)
							res.counts.failed.Add(1)
							err := fmt.Errorf("worker %d: unsolicited reply on actor %d: queue is empty", workerID, req.actorIdx)
							res.recordSemanticError(err)
							t.Errorf("%v", err)
							break
						}
						outcome, err := actState.handleReply(reply, headOp)
						if outcome == replySemanticError {
							res.counts.failedExecution.Add(1)
							res.counts.failed.Add(1)
							res.recordSemanticError(err)
							t.Errorf("worker %d: semantic failure on actor %d: %v", workerID, req.actorIdx, err)
						}
					case <-reqCtx.Done():
						reqCancel()
						if workerCtx.Err() != nil {
							return
						}
						// Request timed out waiting for pending capacity (backpressure rejection before admission)
						res.counts.failedAdmission.Add(1)
						res.counts.failed.Add(1)
						res.endToEndLatency.Add(time.Since(req.scheduledAt))
						continue workerLoop
					case <-workerCtx.Done():
						reqCancel()
						return
					}
				}

				// Select a row ID belonging to this worker's partition
				slot := rng.Int64N(numPartitionRows)
				for tries := 0; tries < int(numPartitionRows); tries++ {
					candID := slot*int64(cfg.workers) + int64(workerID)
					if !actState.isUncertain(candID) {
						slot = candID / int64(cfg.workers)
						break
					}
					slot = (slot + 1) % numPartitionRows
				}
				rowID := slot*int64(cfg.workers) + int64(workerID)

				isPut := req.op == opPut
				var newVal int64
				var prevVal int64
				var writeSeq uint64

				if isPut {
					newVal = rng.Int64N(1_000_000) + 1
					prevVal = actState.getBaseVal(rowID)
					writeSeq = actState.nextWriteSeq(rowID)
				}

				var topic string
				if isPut {
					topic = fmt.Sprintf("put:%d:%d", rowID, newVal)
				} else {
					topic = fmt.Sprintf("get:%d", rowID)
				}

				msg := relayapi.AcquireMessage()
				msg.Topic = topic
				pkg := relayapi.NewMessagePackage(client.pid, targetActor.pid, msg)

				var sendStarted time.Time
				if diagnosticTrace {
					sendStarted = time.Now()
				}
				sendErr := client.cluster.router.Send(pkg)
				if sendErr != nil {
					// Mailbox ingress admission failure (e.g. ErrOverloaded)
					res.counts.failedAdmission.Add(1)
					res.counts.failed.Add(1)
					res.endToEndLatency.Add(time.Since(req.scheduledAt))
					reqCancel()
					continue
				}

				// Successfully admitted to actor mailbox
				res.counts.admitted.Add(1)

				curOp := &pendingOp{
					reqID:       req.id,
					actorIdx:    req.actorIdx,
					op:          req.op,
					rowID:       rowID,
					sentVal:     newVal,
					prevVal:     prevVal,
					writeSeq:    writeSeq,
					scheduledAt: req.scheduledAt,
				}
				if !queue.push(curOp) {
					reqCancel()
					t.Fatalf("worker %d: failed to push admitted op to queue on actor %d: queue full", workerID, req.actorIdx)
				}

			awaitReply:
				for {
					select {
					case reply := <-client.replyCh:
						headOp := queue.pop()
						if headOp == nil {
							res.counts.failedExecution.Add(1)
							res.counts.failed.Add(1)
							err := fmt.Errorf("worker %d: unsolicited reply on actor %d: queue is empty", workerID, req.actorIdx)
							res.recordSemanticError(err)
							t.Errorf("%v", err)
							break awaitReply
						}
						outcome, err := actState.handleReply(reply, headOp)
						if outcome == replySemanticError {
							res.counts.failedExecution.Add(1)
							res.counts.failed.Add(1)
							res.recordSemanticError(err)
							t.Errorf("worker %d: semantic failure on actor %d: %v", workerID, req.actorIdx, err)
							break awaitReply
						}
						if headOp.reqID == curOp.reqID {
							res.counts.completed.Add(1)
							completedAt := time.Now()
							latency := completedAt.Sub(req.scheduledAt)
							res.endToEndLatency.Add(latency)
							// Bound trace volume. These first slow completions diagnose a
							// timeline; they are not a representative latency sample.
							if diagnosticTrace && latency >= 10*time.Millisecond && slowTraceCount.Add(1) <= 64 {
								trace.Logf(reqCtx, "sqlite.slow", "req=%d actor=%d worker=%d scheduled_ns=%d dequeue_ns=%d send_ns=%d completed_ns=%d",
									req.id, req.actorIdx, workerID, req.scheduledAt.Sub(startLoad), now.Sub(startLoad), sendStarted.Sub(startLoad), completedAt.Sub(startLoad))
							}
							break awaitReply
						}
						// headOp was an earlier timed-out request.
						// It has been validated and applied to the oracle.
						// Continue awaiting the current request's reply until deadline.
					case <-reqCtx.Done():
						if workerCtx.Err() != nil {
							reqCancel()
							return
						}
						res.counts.timedout.Add(1)
						res.endToEndLatency.Add(time.Since(req.scheduledAt))
						if isPut {
							actState.addUncertain(rowID, prevVal, newVal)
						}
						break awaitReply
					case <-workerCtx.Done():
						reqCancel()
						return
					}
				}
				reqCancel()
			}
		}(wID)
	}

	// 7. Open-Loop Arrival Generator
	// Schedules arrivals at absolute intervals delta = 1/targetRate from t0.
	// Total planned arrivals are predetermined by declared rate and duration.
	deltaNanos := float64(time.Second) / cfg.targetRate
	totalScheduled := uint64(float64(cfg.duration) * cfg.targetRate / float64(time.Second))
	if totalScheduled == 0 {
		totalScheduled = 1
	}
	res.counts.planned.Store(totalScheduled)
	stopOfferingAt := startLoad.Add(cfg.duration)
	rngGen := rand.New(rand.NewPCG(7771, 333))

	for k := uint64(0); k < totalScheduled; k++ {
		targetTime := startLoad.Add(time.Duration(float64(k) * deltaNanos))
		now := time.Now()
		if now.After(stopOfferingAt) {
			// Window expired: generator missed all remaining scheduled arrivals
			res.counts.generatorDropped.Add(totalScheduled - k)
			break
		}

		if targetTime.After(now) {
			time.Sleep(targetTime.Sub(now))
		}

		actualTime := time.Now()
		if actualTime.After(stopOfferingAt) {
			// Generator lateness pushed beyond offering window: record missed arrivals
			res.counts.generatorDropped.Add(totalScheduled - k)
			break
		}

		res.generatorLateness.Add(actualTime.Sub(targetTime))

		op := opGet
		if rngGen.IntN(100) < cfg.writeRatio {
			op = opPut
		}
		actorIdx := int(k % uint64(cfg.actors))

		req := &openLoopRequest{
			id:          k,
			actorIdx:    actorIdx,
			op:          op,
			scheduledAt: targetTime,
		}

		select {
		case workQueue <- req:
			res.counts.offered.Add(1)
		default:
			res.counts.generatorDropped.Add(1)
		}
	}

	if cfg.gated && stepGateClosed.CompareAndSwap(false, true) {
		close(stepGate)
	}

	// 8. Drain phase: Close queue, wait for workers to process offered work within drain budget
	close(workQueue)

	drainDone := make(chan struct{})
	go func() {
		workerWg.Wait()
		close(drainDone)
	}()

	if err := waitOrCancelWorkers(drainDone, cancelWorkers, cfg.drainBudget); err != nil {
		t.Fatalf("%v", err)
	}

	if gatingClient != nil {
		select {
		case reply := <-gatingClient.replyCh:
			require.Equal(t, "result", reply.Topic)
			gID, _, err := parseGetReply(reply)
			require.NoError(t, err)
			require.Equal(t, int64(0), gID)
		case <-time.After(5 * time.Second):
			t.Fatal("gating client timed out waiting for completion after gate release")
		}
		gatingClient.Close()
	}

	close(stopMemMonitor)
	<-memMonitorDone
	res.duration = time.Since(startLoad)
	stopProfile()
	if diagnosticTrace {
		t.Logf("Diagnostic scheduler workers before=%v after=%v slow_completed=%d (trace limited to first 64)",
			workerStatsBefore, cluster.scheduler.WorkerStats(), slowTraceCount.Load())
	}

	// Every admitted operation still represented in a pending queue must receive
	// and validate its correlated reply before recovery. The single deadline is
	// shared by all worker clients; it is not retried or extended per client.
	replyDrainDeadline := time.Now().Add(cfg.drainBudget)
	for wID := 0; wID < cfg.workers; wID++ {
		for aIdx := 0; aIdx < cfg.actors; aIdx++ {
			client := workerClients[wID][aIdx]
			queue := workerQueues[wID][aIdx]
			actState := workerStates[wID].actors[aIdx]
			if err := drainAcceptedWorkerReplies(client, queue, actState, res, wID, aIdx, replyDrainDeadline, t); err != nil {
				t.Fatalf("%v", err)
			}
			// Keep the prior immediate-reply check after correlated work is gone,
			// so a ready unsolicited reply remains an explicit semantic failure.
			drainWorkerReplies(client, queue, actState, res, wID, aIdx, t)
			if queue.len() != 0 {
				t.Fatalf("worker %d actor %d retained %d pending replies after accepted reply drain", wID, aIdx, queue.len())
			}
			queue.clear()
		}
	}

	// Memory Snapshot: End of load phase
	runtime.ReadMemStats(&res.memEnd)

	// 9. Exact conservation assertion
	res.counts.assertConservation(t)

	// 10. Drain & Recovery: Send recovery probe to each actor
	recovClient := cluster.NewClient("recovery-client")
	defer recovClient.Close()

	for aIdx, a := range actors {
		recovCtx, recovCancel := context.WithTimeout(context.Background(), 10*time.Second)
		recovReply, rDur, err := recovClient.Request(recovCtx, a.pid, "get:0")
		recovCancel()
		require.NoError(t, err, "actor %d must recover and respond after drain", aIdx)
		gotID, _, err := parseGetReply(recovReply)
		require.NoError(t, err)
		require.Equal(t, int64(0), gotID)
		t.Logf("Actor %d recovery verified in %v", aIdx, rDur)
	}

	// 11. Final Database Reconciliation & Per-Key ACK Verification
	for aIdx, a := range actors {
		// A. Reconcile uncertain writes
		for wID := 0; wID < cfg.workers; wID++ {
			actState := workerStates[wID].actors[aIdx]
			actState.mu.Lock()
			uncRows := make(map[int64]map[int64]struct{}, len(actState.uncertain))
			for rID, cands := range actState.uncertain {
				copyCands := make(map[int64]struct{}, len(cands))
				for c := range cands {
					copyCands[c] = struct{}{}
				}
				uncRows[rID] = copyCands
			}
			actState.mu.Unlock()

			for rowID, candSet := range uncRows {
				checkCtx, checkCancel := context.WithTimeout(context.Background(), 10*time.Second)
				checkReply, _, err := recovClient.Request(checkCtx, a.pid, fmt.Sprintf("get:%d", rowID))
				checkCancel()
				require.NoError(t, err)
				gID, gVal, err := parseGetReply(checkReply)
				require.NoError(t, err)
				require.Equal(t, rowID, gID)
				_, valid := candSet[gVal]
				require.True(t, valid,
					"actor %d row %d uncertain write reconciliation: got %d not in candidate set",
					aIdx, rowID, gVal)
				actState.applyAck(rowID, gVal)
			}
		}

		// B. Per-Key Final ACK Verification: GET every touched ACKed row
		for wID := 0; wID < cfg.workers; wID++ {
			actState := workerStates[wID].actors[aIdx]
			actState.mu.Lock()
			ackedCopy := make(map[int64]int64, len(actState.acked))
			for rID, val := range actState.acked {
				ackedCopy[rID] = val
			}
			actState.mu.Unlock()

			for rowID, expectedVal := range ackedCopy {
				checkCtx, checkCancel := context.WithTimeout(context.Background(), 10*time.Second)
				checkReply, _, err := recovClient.Request(checkCtx, a.pid, fmt.Sprintf("get:%d", rowID))
				checkCancel()
				require.NoError(t, err)
				gID, gVal, err := parseGetReply(checkReply)
				require.NoError(t, err)
				require.Equal(t, rowID, gID)
				require.Equal(t, expectedVal, gVal,
					"actor %d row %d per-key ACK mismatch: expected %d, got %d",
					aIdx, rowID, expectedVal, gVal)
			}
		}

		// C. Exact Sum and Count Oracle Check
		initialSum := int64(3) * (cfg.rowsPerActor - 1) * cfg.rowsPerActor / 2
		var totalDelta int64
		for wID := 0; wID < cfg.workers; wID++ {
			actState := workerStates[wID].actors[aIdx]
			actState.mu.Lock()
			for rowID, finalVal := range actState.acked {
				totalDelta += (finalVal - rowID*3)
			}
			actState.mu.Unlock()
		}
		expectedSum := initialSum + totalDelta

		sumCtx, sumCancel := context.WithTimeout(context.Background(), 10*time.Second)
		sumReply, _, err := recovClient.Request(sumCtx, a.pid, "sum")
		sumCancel()
		require.NoError(t, err)
		count, sumVal, err := parseSumReply(sumReply)
		require.NoError(t, err)
		require.Equal(t, cfg.rowsPerActor, count, "final row count for actor %d", aIdx)
		require.Equal(t, expectedSum, sumVal, "final sum oracle match for actor %d", aIdx)

		// SQLite internal memory stats
		statsCtx, statsCancel := context.WithTimeout(context.Background(), 10*time.Second)
		statsReply, _, err := recovClient.Request(statsCtx, a.pid, "stats")
		statsCancel()
		require.NoError(t, err)
		stats, err := parseStatsReply(statsReply)
		require.NoError(t, err)
		require.Greater(t, stats.MemoryUsed, int64(0))
		require.Greater(t, stats.PageCount, int64(0))
		t.Logf("Actor %d SQLite stats: mem_used=%d KB, mem_highwater=%d KB, pages=%d, page_size=%d",
			aIdx, stats.MemoryUsed/1024, stats.MemoryHighwater/1024, stats.PageCount, stats.PageSize)
	}

	// Semantic errors and failed execution must be zero
	require.Empty(t, res.semanticErrors, "must have zero semantic errors / mismatches")
	require.Zero(t, res.semanticDropped.Load(), "must have zero dropped semantic errors")
	require.Zero(t, res.counts.failedExecution.Load(), "failedExecution must be zero even under overload")
	require.Zero(t, cluster.receiver.dropped.Load(), "harness client receiver must have zero dropped replies; dropped replies cause FIFO queue desynchronization")

	// 12. Graceful stop for all actors
	for i, a := range actors {
		require.NoError(t, recovClient.SendOnly(a.pid, "stop"))
		select {
		case sRes := <-a.doneCh:
			require.NotNil(t, sRes)
			require.NoError(t, sRes.Error, "actor %d graceful stop must complete with nil error", i)
		case <-time.After(5 * time.Second):
			t.Fatalf("Actor %d failed to stop within timeout", i)
		}
	}

	// 13. Teardown cluster
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 10*time.Second)
	require.NoError(t, cluster.host.Stop(stopCtx))
	cancelStop()

	// Memory Snapshot: Post-cleanup
	runtime.GC()
	runtime.ReadMemStats(&res.memCleanup)

	p50, p95, p99 := res.endToEndLatency.Percentiles()
	lp50, lp95, lp99 := res.generatorLateness.Percentiles()
	thp := float64(res.counts.completed.Load()) / res.duration.Seconds()

	t.Logf("================ Open-Loop Load Proof Results ================")
	t.Logf("Duration:           %v (Target Rate: %.0f req/s)", res.duration, cfg.targetRate)
	t.Logf("Conservation:       Planned=%d = Offered(%d) + Dropped(%d)",
		res.counts.planned.Load(), res.counts.offered.Load(), res.counts.generatorDropped.Load())
	t.Logf("Execution:          Offered=%d = Admitted(%d) + Rejected(%d)",
		res.counts.offered.Load(), res.counts.admitted.Load(), res.counts.failedAdmission.Load())
	t.Logf("Outcomes:           Admitted=%d = Completed(%d) + FailedExec(%d) + TimedOut(%d)",
		res.counts.admitted.Load(), res.counts.completed.Load(), res.counts.failedExecution.Load(), res.counts.timedout.Load())
	t.Logf("Completed Thp:      %.2f ops/sec", thp)
	t.Logf("Generator Lateness: p50=%v, p95=%v, p99=%v (samples=%d)",
		lp50, lp95, lp99, len(res.generatorLateness.samples))
	t.Logf("Dispatched Latency: p50=%v, p95=%v, p99=%v (samples=%d, scheduled arrival through outcome; generator drops omitted)",
		p50, p95, p99, len(res.endToEndLatency.samples))
	t.Logf("Memory Baseline:    HeapAlloc=%d KB, Sys=%d KB",
		res.memBaseline.HeapAlloc/1024, res.memBaseline.Sys/1024)
	t.Logf("Memory Peak:        HeapAlloc=%d KB", res.memSeries.PeakHeap()/1024)
	t.Logf("Memory End:         HeapAlloc=%d KB, Sys=%d KB",
		res.memEnd.HeapAlloc/1024, res.memEnd.Sys/1024)
	t.Logf("Memory Cleanup:     HeapAlloc=%d KB, Sys=%d KB (NumGC delta=%d)",
		res.memCleanup.HeapAlloc/1024, res.memCleanup.Sys/1024, res.memCleanup.NumGC-res.memBaseline.NumGC)
	t.Logf("Memory Slope:       %.2f bytes/sec over %d samples",
		res.memSeries.Slope(), len(res.memSeries.samples))
	t.Logf("==============================================================")

	return res
}

// --- Test Suites ---

// TestSQLiteActor_OpenLoop_CorrectnessAndOverload is a short deterministic test verifying:
// 1. In-capacity open-loop arrivals with zero drops, zero timeouts, and exact conservation.
// 2. Overload open-loop arrivals exposing bounded queue drops, mailbox rejection, and timeouts.
// 3. Strict accounting conservation: planned == completed + failed + timedout + generatorDropped.
// 4. Drain, recovery, per-key final ACK verification, and final database reconciliation.
func TestSQLiteActor_OpenLoop_CorrectnessAndOverload(t *testing.T) {
	t.Run("SteadyInCapacity", func(t *testing.T) {
		cfg := openLoopConfig{
			actors:        1,
			rowsPerActor:  500,
			workers:       2,
			queueCapacity: 64,
			targetRate:    100, // 100 req/s
			duration:      500 * time.Millisecond,
			reqTimeout:    2 * time.Second,
			drainBudget:   5 * time.Second,
			writeRatio:    20, // 80% GET, 20% PUT
			mailboxLimits: nil,
		}
		res := runOpenLoopLoad(t, cfg)
		require.Greater(t, res.counts.completed.Load(), uint64(0))
		require.Zero(t, res.counts.generatorDropped.Load(), "no generator drops under in-capacity load")
		require.Zero(t, res.counts.failed.Load(), "no failures under in-capacity load")
		require.Zero(t, res.counts.timedout.Load(), "no timeouts under in-capacity load")
		require.Zero(t, res.counts.failedExecution.Load(), "no semantic failures")
		require.Equal(t, res.counts.planned.Load(), res.counts.completed.Load())
	})

	t.Run("OverloadBurstWithDropsAndTimeouts", func(t *testing.T) {
		tightMailbox := actorhost.Limits{
			Capacity:     4,
			Bytes:        64 * 1024,
			MessageBytes: 4096,
		}
		cfg := openLoopConfig{
			actors:           1,
			rowsPerActor:     500,
			workers:          2,
			queueCapacity:    8,
			targetRate:       500, // 500 req/s into 2 workers with queue 8
			duration:         250 * time.Millisecond,
			reqTimeout:       30 * time.Millisecond,
			drainBudget:      5 * time.Second,
			writeRatio:       30,
			mailboxLimits:    &tightMailbox,
			gated:            true,
			gateHoldDuration: 50 * time.Millisecond,
		}
		res := runOpenLoopLoad(t, cfg)
		require.Greater(t, res.counts.planned.Load(), uint64(0))
		require.Greater(t, res.counts.generatorDropped.Load(), uint64(0),
			"must drop requests at generator when bounded queue is saturated")
		require.Greater(t, res.counts.failedAdmission.Load(), uint64(0),
			"must reject requests at mailbox ingress when mailbox is saturated")
		require.Greater(t, res.counts.timedout.Load(), uint64(0),
			"must observe timeouts for admitted requests while actor is gated")
		require.Greater(t, res.counts.completed.Load(), uint64(0),
			"must complete requests after gate release and drain")
		require.Zero(t, res.counts.failedExecution.Load(),
			"failed execution (malformed/wrong result values) must be zero even under overload")
	})
}

// --- Deterministic Fault-Injection & Accounting Unit Tests ---

func TestSQLiteOpenLoop_Oracle_RejectsBadGetValue(t *testing.T) {
	state := newPartitionActorState()

	// 1. Clean row 5 has initial value 15 (5*3)
	msgValidClean := relayapi.AcquireMessage()
	msgValidClean.Topic = "result"
	msgValidClean.Payloads = append(msgValidClean.Payloads, parseToPayload("value:5:15"))
	outcome, err := state.handleReply(msgValidClean, &pendingOp{op: opGet, rowID: 5})
	require.NoError(t, err)
	require.Equal(t, replyMatch, outcome)

	// Bad GET value for clean row 5: returns 999 instead of 15
	msgBadClean := relayapi.AcquireMessage()
	msgBadClean.Topic = "result"
	msgBadClean.Payloads = append(msgBadClean.Payloads, parseToPayload("value:5:999"))
	outcome, err = state.handleReply(msgBadClean, &pendingOp{op: opGet, rowID: 5})
	require.Error(t, err)
	require.Equal(t, replySemanticError, outcome)

	// 2. Set acknowledged value for row 5 to 42
	state.applyAck(5, 42)
	msgValidAcked := relayapi.AcquireMessage()
	msgValidAcked.Topic = "result"
	msgValidAcked.Payloads = append(msgValidAcked.Payloads, parseToPayload("value:5:42"))
	outcome, err = state.handleReply(msgValidAcked, &pendingOp{op: opGet, rowID: 5})
	require.NoError(t, err)
	require.Equal(t, replyMatch, outcome)

	// Bad GET value for acknowledged row 5: returns stale initial value 15
	msgStaleAcked := relayapi.AcquireMessage()
	msgStaleAcked.Topic = "result"
	msgStaleAcked.Payloads = append(msgStaleAcked.Payloads, parseToPayload("value:5:15"))
	outcome, err = state.handleReply(msgStaleAcked, &pendingOp{op: opGet, rowID: 5})
	require.Error(t, err)
	require.Equal(t, replySemanticError, outcome)

	// 3. Mark row 5 uncertain with candidates {42, 100}
	state.addUncertain(5, 42, 100)
	msgValidUnc1 := relayapi.AcquireMessage()
	msgValidUnc1.Topic = "result"
	msgValidUnc1.Payloads = append(msgValidUnc1.Payloads, parseToPayload("value:5:42"))
	outcome, err = state.handleReply(msgValidUnc1, &pendingOp{op: opGet, rowID: 5})
	require.NoError(t, err)
	require.Equal(t, replyMatch, outcome)

	msgValidUnc2 := relayapi.AcquireMessage()
	msgValidUnc2.Topic = "result"
	msgValidUnc2.Payloads = append(msgValidUnc2.Payloads, parseToPayload("value:5:100"))
	outcome, err = state.handleReply(msgValidUnc2, &pendingOp{op: opGet, rowID: 5})
	require.NoError(t, err)
	require.Equal(t, replyMatch, outcome)

	// Bad GET value for uncertain row: 77 is not in candidate set {42, 100}
	msgBadUnc := relayapi.AcquireMessage()
	msgBadUnc.Topic = "result"
	msgBadUnc.Payloads = append(msgBadUnc.Payloads, parseToPayload("value:5:77"))
	outcome, err = state.handleReply(msgBadUnc, &pendingOp{op: opGet, rowID: 5})
	require.Error(t, err)
	require.Equal(t, replySemanticError, outcome)

	// 4. Unexpected topic or malformed text fails
	msgErr := relayapi.AcquireMessage()
	msgErr.Topic = "error"
	msgErr.Payloads = append(msgErr.Payloads, parseToPayload("guest query failed"))
	outcome, err = state.handleReply(msgErr, &pendingOp{op: opGet, rowID: 5})
	require.Error(t, err)
	require.Equal(t, replySemanticError, outcome)
}

func TestSQLiteOpenLoop_Oracle_UncertainRepeatedWritesNotForgotten(t *testing.T) {
	state := newPartitionActorState()

	// Row 10 base value = 30
	// Write 1 times out: newVal=101
	state.addUncertain(10, 30, 101)
	// Write 2 to same row times out: newVal=102 (must not overwrite candidate set)
	state.addUncertain(10, 101, 102)
	// Write 3 to same row times out: newVal=103 (must not overwrite candidate set)
	state.addUncertain(10, 102, 103)

	// All 4 candidate values {30, 101, 102, 103} must be preserved
	for _, val := range []int64{30, 101, 102, 103} {
		require.True(t, state.isValidValue(10, val), "value %d must be recognized as valid candidate", val)
	}
	require.False(t, state.isValidValue(10, 999), "unrelated value 999 must be rejected")

	// Late ACK arrives for write 2 (102)
	ackMsg := relayapi.AcquireMessage()
	ackMsg.Topic = "result"
	ackMsg.Payloads = append(ackMsg.Payloads, parseToPayload("updated:10:102"))
	outcome, err := state.handleReply(ackMsg, &pendingOp{op: opPut, rowID: 10, sentVal: 102, writeSeq: 2})
	require.NoError(t, err)
	require.Equal(t, replyMatch, outcome)

	// After ACK, row 10 is authoritative 102, uncertainty is cleared
	require.False(t, state.isUncertain(10))
	require.True(t, state.isValidValue(10, 102))
	require.False(t, state.isValidValue(10, 30))
	require.False(t, state.isValidValue(10, 101))
	require.False(t, state.isValidValue(10, 103))
}

func TestSQLiteOpenLoop_Accounting_MissingArrivalsCounted(t *testing.T) {
	targetRate := 100.0
	duration := 200 * time.Millisecond
	deltaNanos := float64(time.Second) / targetRate
	totalScheduled := uint64(float64(duration) * targetRate / float64(time.Second))
	require.Equal(t, uint64(20), totalScheduled)

	var counts openLoopCounts
	counts.planned.Store(totalScheduled)

	// Bounded queue with capacity 5
	workQueue := make(chan uint64, 5)
	start := time.Now()
	stopAt := start.Add(duration)

	for k := uint64(0); k < totalScheduled; k++ {
		now := time.Now()
		if now.After(stopAt) {
			counts.generatorDropped.Add(totalScheduled - k)
			break
		}
		targetTime := start.Add(time.Duration(float64(k) * deltaNanos))
		if targetTime.After(now) {
			time.Sleep(targetTime.Sub(now))
		}
		if time.Now().After(stopAt) {
			counts.generatorDropped.Add(totalScheduled - k)
			break
		}

		select {
		case workQueue <- k:
			counts.offered.Add(1)
		default:
			counts.generatorDropped.Add(1)
		}
	}

	p := counts.planned.Load()
	off := counts.offered.Load()
	gd := counts.generatorDropped.Load()

	require.Equal(t, totalScheduled, p)
	require.Equal(t, p, off+gd, "conservation: planned (%d) == offered (%d) + generator_dropped (%d)", p, off, gd)
	require.Equal(t, uint64(5), off, "work queue had capacity 5, offered must equal 5")
	require.Equal(t, uint64(15), gd, "all remaining 15 arrivals must be counted as generatorDropped")
}

func TestSQLiteOpenLoop_Deadlines_Bounded(t *testing.T) {
	// 1. Verify rows >= workers validation panics on violation
	badCfg := openLoopConfig{
		actors:       1,
		rowsPerActor: 2,
		workers:      4, // workers > rowsPerActor
		targetRate:   10,
		duration:     100 * time.Millisecond,
	}
	require.Panics(t, func() {
		validateConfig(badCfg)
	})

	// 2. Test queue deadline expiry before dequeue
	reqTimeout := 50 * time.Millisecond
	scheduledAt := time.Now().Add(-100 * time.Millisecond) // scheduled in past
	deadline := scheduledAt.Add(reqTimeout)

	now := time.Now()
	require.False(t, deadline.After(now), "deadline must be expired before worker dequeue")

	var counts openLoopCounts
	counts.offered.Add(1)
	if !deadline.After(now) {
		counts.failedAdmission.Add(1)
		counts.failed.Add(1)
	}
	require.Equal(t, counts.offered.Load(), counts.failedAdmission.Load())
	require.Zero(t, counts.admitted.Load(), "expired queue request must not be admitted to actor")
}

func parseToPayload(s string) payload.Payload {
	return payload.NewPayload([]byte(s), payload.String)
}

// TestSQLiteActorLoad_OpenLoop_Sustained is an opt-in configurable open-loop load test.
// Run with: WIPPY_SQLITE_OPEN_LOOP=1 go test ./runtime/wasm/engine -run '^TestSQLiteActorLoad_OpenLoop_Sustained$' -v
func TestSQLiteActorLoad_OpenLoop_Sustained(t *testing.T) {
	if os.Getenv("WIPPY_SQLITE_OPEN_LOOP") == "" && os.Getenv("WIPPY_SQLITE_LOAD") == "" {
		t.Skip("skipping sustained open-loop load test; set WIPPY_SQLITE_OPEN_LOOP=1 to run")
	}

	targetRate := 500.0
	if envRate := os.Getenv("WIPPY_OPEN_LOOP_RATE"); envRate != "" {
		r, err := strconv.ParseFloat(envRate, 64)
		require.NoError(t, err, "parse WIPPY_OPEN_LOOP_RATE")
		targetRate = r
	}
	require.True(t, targetRate >= 1.0 && targetRate <= 25000.0, "WIPPY_OPEN_LOOP_RATE must be in range 1..25000")

	duration := 10 * time.Second
	if envDur := os.Getenv("WIPPY_OPEN_LOOP_DURATION"); envDur != "" {
		d, err := time.ParseDuration(envDur)
		require.NoError(t, err, "parse WIPPY_OPEN_LOOP_DURATION")
		duration = d
	}
	require.True(t, duration >= time.Second && duration <= 5*time.Minute, "WIPPY_OPEN_LOOP_DURATION must be between 1s and 5m")

	workers := 4
	if envWork := os.Getenv("WIPPY_OPEN_LOOP_WORKERS"); envWork != "" {
		w, err := strconv.Atoi(envWork)
		require.NoError(t, err, "parse WIPPY_OPEN_LOOP_WORKERS")
		workers = w
	}
	require.True(t, workers >= 1 && workers <= 64, "WIPPY_OPEN_LOOP_WORKERS must be in range 1..64")

	queueCapacity := 256
	if envQ := os.Getenv("WIPPY_OPEN_LOOP_QUEUE"); envQ != "" {
		q, err := strconv.Atoi(envQ)
		require.NoError(t, err, "parse WIPPY_OPEN_LOOP_QUEUE")
		queueCapacity = q
	}
	require.True(t, queueCapacity >= 1 && queueCapacity <= 4096, "WIPPY_OPEN_LOOP_QUEUE must be in range 1..4096")

	rows := int64(10000)
	if envRows := os.Getenv("WIPPY_OPEN_LOOP_ROWS"); envRows != "" {
		r, err := strconv.ParseInt(envRows, 10, 64)
		require.NoError(t, err, "parse WIPPY_OPEN_LOOP_ROWS")
		rows = r
	}
	require.True(t, rows >= 1 && rows <= 1000000, "WIPPY_OPEN_LOOP_ROWS must be in range 1..1000000")
	require.True(t, rows >= int64(workers), "rows per actor must be >= workers")

	actors := 2
	if envAct := os.Getenv("WIPPY_OPEN_LOOP_ACTORS"); envAct != "" {
		a, err := strconv.Atoi(envAct)
		require.NoError(t, err, "parse WIPPY_OPEN_LOOP_ACTORS")
		actors = a
	}
	require.True(t, actors >= 1 && actors <= 16, "WIPPY_OPEN_LOOP_ACTORS must be in range 1..16")

	reqTimeout := 2 * time.Second
	if envTo := os.Getenv("WIPPY_OPEN_LOOP_TIMEOUT"); envTo != "" {
		to, err := time.ParseDuration(envTo)
		require.NoError(t, err, "parse WIPPY_OPEN_LOOP_TIMEOUT")
		reqTimeout = to
	}
	require.True(t, reqTimeout >= 10*time.Millisecond && reqTimeout <= 30*time.Second, "WIPPY_OPEN_LOOP_TIMEOUT must be between 10ms and 30s")

	drainBudget := 10 * time.Second
	if envDB := os.Getenv("WIPPY_OPEN_LOOP_DRAIN"); envDB != "" {
		db, err := time.ParseDuration(envDB)
		require.NoError(t, err, "parse WIPPY_OPEN_LOOP_DRAIN")
		drainBudget = db
	}

	maxPending := 64
	if envMp := os.Getenv("WIPPY_OPEN_LOOP_MAX_PENDING"); envMp != "" {
		mp, err := strconv.Atoi(envMp)
		require.NoError(t, err, "parse WIPPY_OPEN_LOOP_MAX_PENDING")
		maxPending = mp
	}
	require.True(t, maxPending >= 1 && maxPending <= 1024, "WIPPY_OPEN_LOOP_MAX_PENDING must be in range 1..1024")

	cfg := openLoopConfig{
		actors:        actors,
		rowsPerActor:  rows,
		workers:       workers,
		queueCapacity: queueCapacity,
		maxPending:    maxPending,
		targetRate:    targetRate,
		duration:      duration,
		reqTimeout:    reqTimeout,
		drainBudget:   drainBudget,
		writeRatio:    20,
		mailboxLimits: nil,
	}

	t.Logf("Starting Sustained Open-Loop Load Test: Rate=%.0f req/s, Duration=%v, Workers=%d, Queue=%d, Rows=%d, Actors=%d",
		targetRate, duration, workers, queueCapacity, rows, actors)

	res := runOpenLoopLoad(t, cfg)
	require.Greater(t, res.counts.completed.Load(), uint64(0), "must complete requests during sustained run")
}

// startOpenLoopProfile excludes compilation/seeding and post-load verification.
// Profiling is opt-in and its timings are diagnostic, not acceptance samples.
func startOpenLoopProfile(t testing.TB) func() {
	t.Helper()
	var stops []func()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			for i := len(stops) - 1; i >= 0; i-- {
				stops[i]()
			}
		})
	}
	t.Cleanup(stop)
	for _, profile := range []struct {
		start func(*os.File) error
		stop  func()
		env   string
	}{
		{func(f *os.File) error { return pprof.StartCPUProfile(f) }, pprof.StopCPUProfile, "WIPPY_OPEN_LOOP_CPU_PROFILE"},
		{func(f *os.File) error { return trace.Start(f) }, trace.Stop, "WIPPY_OPEN_LOOP_TRACE"},
	} {
		path := os.Getenv(profile.env)
		if path == "" {
			continue
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		require.NoError(t, err, "create %s", profile.env)
		if err := profile.start(f); err != nil {
			_ = f.Close()
			t.Fatalf("start %s: %v", profile.env, err)
		}
		stops = append(stops, func() {
			profile.stop()
			if err := f.Close(); err != nil {
				t.Errorf("close %s: %v", profile.env, err)
			}
		})
	}
	return stop
}

func TestSQLiteOpenLoop_DrainPendingReplies_WaitsForLateACK(t *testing.T) {
	client := &harnessWorkerClient{replyCh: make(chan *relayapi.Message, 1)}
	queue := newPendingQueue(1)
	state := newPartitionActorState()
	res := &openLoopResult{}
	res.counts.timedout.Add(1)

	op := &pendingOp{reqID: 17, op: opPut, rowID: 4, sentVal: 91, writeSeq: 1}
	require.True(t, queue.push(op))
	reply := relayapi.AcquireMessage()
	reply.Topic = "result"
	reply.Payloads = append(reply.Payloads, parseToPayload("updated:4:91"))
	go func() {
		time.Sleep(10 * time.Millisecond)
		client.replyCh <- reply
	}()

	err := drainAcceptedWorkerReplies(client, queue, state, res, 0, 0, time.Now().Add(time.Second), t)
	require.NoError(t, err)
	require.Zero(t, queue.len(), "the late reply must clear its admitted pending operation")
	require.Equal(t, int64(91), state.getBaseVal(4), "the late ACK must update the reference state")
	require.Equal(t, uint64(1), res.counts.timedout.Load(), "a late ACK must not relabel the original timeout")
	require.Zero(t, res.counts.completed.Load(), "post-timeout draining does not create a completed request")
	require.Zero(t, res.counts.failedExecution.Load())
}

func TestSQLiteOpenLoop_DrainPendingReplies_DeadlineRetainsPendingEvidence(t *testing.T) {
	client := &harnessWorkerClient{replyCh: make(chan *relayapi.Message, 1)}
	queue := newPendingQueue(1)
	state := newPartitionActorState()
	res := &openLoopResult{}
	pending := &pendingOp{reqID: 29, op: opGet, rowID: 6}
	require.True(t, queue.push(pending))

	err := drainAcceptedWorkerReplies(client, queue, state, res, 2, 1, time.Now().Add(10*time.Millisecond), t)
	require.Error(t, err)
	require.ErrorContains(t, err, "accepted reply drain exceeded shared budget")
	require.Equal(t, 1, queue.len(), "deadline failure must retain the accepted pending operation for diagnosis")
	require.Same(t, pending, queue.ops[queue.head])
	require.Zero(t, res.counts.completed.Load())
	require.Zero(t, res.counts.failedExecution.Load())
}

func TestSQLiteOpenLoop_DrainPendingReplies_ExpiredDeadlineRetainsBufferedReply(t *testing.T) {
	client := &harnessWorkerClient{replyCh: make(chan *relayapi.Message, 1)}
	queue := newPendingQueue(1)
	state := newPartitionActorState()
	res := &openLoopResult{}
	pending := &pendingOp{reqID: 31, op: opPut, rowID: 8, sentVal: 72, writeSeq: 1}
	require.True(t, queue.push(pending))
	reply := relayapi.AcquireMessage()
	reply.Topic = "result"
	reply.Payloads = append(reply.Payloads, parseToPayload("updated:8:72"))
	client.replyCh <- reply

	err := drainAcceptedWorkerReplies(client, queue, state, res, 3, 0, time.Now().Add(-time.Nanosecond), t)
	require.Error(t, err)
	require.Equal(t, 1, queue.len())
	require.Same(t, pending, queue.ops[queue.head])
	require.Equal(t, 1, len(client.replyCh), "expired drain must not take a buffered reply")
	require.Equal(t, int64(24), state.getBaseVal(8), "expired drain must not apply the late ACK")
	require.Zero(t, res.counts.failedExecution.Load())
}

func TestSQLiteOpenLoop_DrainPendingReplies_ClientsShareOneDeadline(t *testing.T) {
	firstClient := &harnessWorkerClient{replyCh: make(chan *relayapi.Message, 1)}
	firstQueue := newPendingQueue(1)
	firstState := newPartitionActorState()
	res := &openLoopResult{}
	require.True(t, firstQueue.push(&pendingOp{reqID: 41, op: opGet, rowID: 9}))

	sharedDeadline := time.Now().Add(10 * time.Millisecond)
	err := drainAcceptedWorkerReplies(firstClient, firstQueue, firstState, res, 0, 0, sharedDeadline, t)
	require.Error(t, err)
	require.Equal(t, 1, firstQueue.len())

	secondClient := &harnessWorkerClient{replyCh: make(chan *relayapi.Message, 1)}
	secondQueue := newPendingQueue(1)
	secondState := newPartitionActorState()
	pending := &pendingOp{reqID: 42, op: opPut, rowID: 10, sentVal: 83, writeSeq: 1}
	require.True(t, secondQueue.push(pending))
	reply := relayapi.AcquireMessage()
	reply.Topic = "result"
	reply.Payloads = append(reply.Payloads, parseToPayload("updated:10:83"))
	secondClient.replyCh <- reply

	err = drainAcceptedWorkerReplies(secondClient, secondQueue, secondState, res, 0, 1, sharedDeadline, t)
	require.Error(t, err, "the second client must not receive a fresh drain budget")
	require.Equal(t, 1, secondQueue.len())
	require.Same(t, pending, secondQueue.ops[secondQueue.head])
	require.Equal(t, 1, len(secondClient.replyCh), "expired shared deadline must leave the second reply buffered")
	require.Equal(t, int64(30), secondState.getBaseVal(10))
}

func TestSQLiteOpenLoop_DrainPendingReplies_RejectsUnsolicitedReply(t *testing.T) {
	client := &harnessWorkerClient{replyCh: make(chan *relayapi.Message, 1)}
	queue := newPendingQueue(1)
	state := newPartitionActorState()
	reply := relayapi.AcquireMessage()
	reply.Topic = "result"
	reply.Payloads = append(reply.Payloads, parseToPayload("value:3:9"))
	client.replyCh <- reply

	outcome, err := consumeWorkerReply(<-client.replyCh, queue, state, 1, 2)
	require.Equal(t, workerReplyUnsolicited, outcome)
	require.Error(t, err)
	require.ErrorContains(t, err, "unsolicited reply")
	require.Zero(t, queue.len())
}
