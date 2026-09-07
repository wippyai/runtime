// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	relayapi "github.com/wippyai/runtime/api/relay"
)

// TestOracleCorrelation_RejectsArbitraryPutValue verifies that handleReply strictly validates
// that the reply value matches the sent newVal in the correlated pending operation.
func TestOracleCorrelation_RejectsArbitraryPutValue(t *testing.T) {
	state := newPartitionActorState()

	// Correlated operation: PUT row 5, sent newVal = 100
	op := &pendingOp{
		reqID:    1,
		actorIdx: 0,
		op:       opPut,
		rowID:    5,
		sentVal:  100,
		writeSeq: 1,
	}

	// Case 1: Reply value matches sentVal (100) -> SUCCESS
	goodReply := relayapi.AcquireMessage()
	goodReply.Topic = "result"
	goodReply.Payloads = append(goodReply.Payloads, parseToPayload("updated:5:100"))

	outcome, err := state.handleReply(goodReply, op)
	require.NoError(t, err)
	require.Equal(t, replyMatch, outcome)
	require.Equal(t, int64(100), state.acked[5])

	// Case 2: Reply value is arbitrary / corrupted (999) -> REJECTED as semantic error
	op2 := &pendingOp{
		reqID:    2,
		actorIdx: 0,
		op:       opPut,
		rowID:    5,
		sentVal:  200,
		writeSeq: 2,
	}
	badReply := relayapi.AcquireMessage()
	badReply.Topic = "result"
	badReply.Payloads = append(badReply.Payloads, parseToPayload("updated:5:999"))

	outcome, err = state.handleReply(badReply, op2)
	require.Error(t, err)
	require.Equal(t, replySemanticError, outcome, "mismatched PUT value must return replySemanticError")
	require.Contains(t, err.Error(), "put reply value mismatch")
	require.NotEqual(t, int64(999), state.acked[5], "oracle must not record arbitrary value in acked state")
	require.Equal(t, int64(100), state.acked[5], "acked state must remain unchanged")
}

// TestOracleCorrelation_LateOldAckMustNotRegressState proves that when a newer write
// has already been acknowledged, a late reply from an older write does not regress acked state.
func TestOracleCorrelation_LateOldAckMustNotRegressState(t *testing.T) {
	state := newPartitionActorState()

	// Write 1: newVal = 100, seq = 1 (times out)
	opWrite1 := &pendingOp{
		reqID:    1,
		actorIdx: 0,
		op:       opPut,
		rowID:    5,
		sentVal:  100,
		prevVal:  15,
		writeSeq: 1,
	}
	state.addUncertain(5, 15, 100)

	// Write 2: newVal = 200, seq = 2 (completes within deadline)
	opWrite2 := &pendingOp{
		reqID:    2,
		actorIdx: 0,
		op:       opPut,
		rowID:    5,
		sentVal:  200,
		prevVal:  15,
		writeSeq: 2,
	}
	replyWrite2 := relayapi.AcquireMessage()
	replyWrite2.Topic = "result"
	replyWrite2.Payloads = append(replyWrite2.Payloads, parseToPayload("updated:5:200"))

	outcome2, err := state.handleReply(replyWrite2, opWrite2)
	require.NoError(t, err)
	require.Equal(t, replyMatch, outcome2)
	require.Equal(t, int64(200), state.acked[5])

	// Now late reply for Write 1 arrives: updated:5:100 (seq = 1)
	replyWrite1 := relayapi.AcquireMessage()
	replyWrite1.Topic = "result"
	replyWrite1.Payloads = append(replyWrite1.Payloads, parseToPayload("updated:5:100"))

	outcome1, err := state.handleReply(replyWrite1, opWrite1)
	require.NoError(t, err)
	_ = outcome1

	// Sound oracle check: acked[5] must REMAIN 200 (never regressed by late older ACK)
	require.Equal(t, int64(200), state.acked[5], "late older ACK must never overwrite newer acknowledged state")
	require.False(t, state.isUncertain(5), "uncertainty must remain cleared after newer write ACK")
}

// TestOracleCorrelation_ResidualDrainIsolatesActors proves that per-actor queues and clients
// prevent residual drain from broadcasting replies intended for one actor to other actors.
func TestOracleCorrelation_ResidualDrainIsolatesActors(t *testing.T) {
	actor0State := newPartitionActorState()
	actor1State := newPartitionActorState()

	actor0Queue := newPendingQueue(64)
	actor1Queue := newPendingQueue(64)

	// Actor 0 had an admitted PUT request that timed out:
	opActor0 := &pendingOp{
		reqID:    10,
		actorIdx: 0,
		op:       opPut,
		rowID:    5,
		sentVal:  500,
		writeSeq: 1,
	}
	actor0Queue.push(opActor0)

	// In residual drain, Actor 0 receives its reply:
	rep0 := relayapi.AcquireMessage()
	rep0.Topic = "result"
	rep0.Payloads = append(rep0.Payloads, parseToPayload("updated:5:500"))

	// Drain actor 0:
	head0 := actor0Queue.pop()
	require.NotNil(t, head0)
	outcome0, err := actor0State.handleReply(rep0, head0)
	require.NoError(t, err)
	require.Equal(t, replyMatch, outcome0)
	require.Equal(t, int64(500), actor0State.acked[5])

	// Drain actor 1: queue is empty, no messages arrived for actor 1
	require.Equal(t, 0, actor1Queue.len())
	require.Equal(t, int64(0), actor1State.acked[5], "actor 1 state must not be mutated by actor 0 drain")
	require.True(t, actor1State.isValidValue(5, 15), "actor 1 row 5 must remain at clean initial value (15)")
}

// TestOracleCorrelation_PendingQueueFIFOOrdering proves that FIFO queue correlation
// matches replies in the exact order requests were admitted to the actor.
func TestOracleCorrelation_PendingQueueFIFOOrdering(t *testing.T) {
	state := newPartitionActorState()
	queue := newPendingQueue(64)

	op1 := &pendingOp{reqID: 1, op: opGet, rowID: 10}
	op2 := &pendingOp{reqID: 2, op: opPut, rowID: 10, sentVal: 777, writeSeq: 1}
	op3 := &pendingOp{reqID: 3, op: opGet, rowID: 10}

	queue.push(op1)
	queue.push(op2)
	queue.push(op3)
	require.Equal(t, 3, queue.len())

	// Reply 1: value:10:30
	rep1 := relayapi.AcquireMessage()
	rep1.Topic = "result"
	rep1.Payloads = append(rep1.Payloads, parseToPayload("value:10:30"))
	head1 := queue.pop()
	require.Equal(t, uint64(1), head1.reqID)
	out1, err := state.handleReply(rep1, head1)
	require.NoError(t, err)
	require.Equal(t, replyMatch, out1)

	// Reply 2: updated:10:777
	rep2 := relayapi.AcquireMessage()
	rep2.Topic = "result"
	rep2.Payloads = append(rep2.Payloads, parseToPayload("updated:10:777"))
	head2 := queue.pop()
	require.Equal(t, uint64(2), head2.reqID)
	out2, err := state.handleReply(rep2, head2)
	require.NoError(t, err)
	require.Equal(t, replyMatch, out2)

	// Reply 3: value:10:777
	rep3 := relayapi.AcquireMessage()
	rep3.Topic = "result"
	rep3.Payloads = append(rep3.Payloads, parseToPayload("value:10:777"))
	head3 := queue.pop()
	require.Equal(t, uint64(3), head3.reqID)
	out3, err := state.handleReply(rep3, head3)
	require.NoError(t, err)
	require.Equal(t, replyMatch, out3)

	require.Equal(t, 0, queue.len())
}

// TestOracleCorrelation_UnsolicitedReplyRejected proves that unsolicited replies
// (when pending queue is empty) are rejected with semantic error.
func TestOracleCorrelation_UnsolicitedReplyRejected(t *testing.T) {
	state := newPartitionActorState()
	queue := newPendingQueue(64)

	unsolicitedMsg := relayapi.AcquireMessage()
	unsolicitedMsg.Topic = "result"
	unsolicitedMsg.Payloads = append(unsolicitedMsg.Payloads, parseToPayload("value:5:15"))

	head := queue.pop()
	require.Nil(t, head)

	outcome, err := state.handleReply(unsolicitedMsg, head)
	require.Error(t, err)
	require.Equal(t, replySemanticError, outcome)
	require.Contains(t, err.Error(), "unsolicited reply")
}

// TestOracleCorrelation_BoundedSemanticErrorStorage proves that semantic error storage
// is strictly bounded to maxRecordedSemanticErrors, and excess errors increment semanticDropped.
func TestOracleCorrelation_BoundedSemanticErrorStorage(t *testing.T) {
	res := &openLoopResult{}

	// Record 100 errors (exceeding maxRecordedSemanticErrors = 64)
	for i := 0; i < 100; i++ {
		res.recordSemanticError(fmt.Errorf("error %d", i))
	}

	res.semanticMu.Lock()
	defer res.semanticMu.Unlock()
	require.Equal(t, maxRecordedSemanticErrors, len(res.semanticErrors), "recorded errors must be capped at 64")
	require.Equal(t, uint64(36), res.semanticDropped.Load(), "excess errors must be counted as dropped")
}

// TestOracleCorrelation_WaitOrCancelWorkers exercises the actual waitOrCancelWorkers helper
// used by runOpenLoopLoad, testing both clean completion and timeout cancellation.
func TestOracleCorrelation_WaitOrCancelWorkers(t *testing.T) {
	t.Run("CleanDrainWithinBudget", func(t *testing.T) {
		drainDone := make(chan struct{})
		cancelCalled := false
		cancelWorkers := func() { cancelCalled = true }

		go func() {
			time.Sleep(10 * time.Millisecond)
			close(drainDone)
		}()

		err := waitOrCancelWorkers(drainDone, cancelWorkers, 500*time.Millisecond)
		require.NoError(t, err, "must return nil error on clean drain within budget")
		require.False(t, cancelCalled, "cancelWorkers must not be called when drain finishes within budget")
	})

	t.Run("DrainTimeoutCancelsAndJoinsOnSameDrainDone", func(t *testing.T) {
		drainDone := make(chan struct{})
		workerExited := make(chan struct{})
		var workerWg sync.WaitGroup

		workerCtx, cancelWorkers := context.WithCancel(context.Background())
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			select {
			case <-workerCtx.Done():
				close(workerExited)
				return
			case <-time.After(10 * time.Second):
				return
			}
		}()

		// Existing drainDone channel waiter (matches runOpenLoopLoad architecture)
		go func() {
			workerWg.Wait()
			close(drainDone)
		}()

		start := time.Now()
		err := waitOrCancelWorkers(drainDone, cancelWorkers, 30*time.Millisecond)
		elapsed := time.Since(start)

		require.Error(t, err, "must return error on drain budget expiry")
		require.Contains(t, err.Error(), "worker drain exceeded bounded budget")

		// Verify worker exited and joined
		select {
		case <-workerExited:
		default:
			t.Fatal("worker did not exit upon cancellation")
		}

		select {
		case <-drainDone:
		default:
			t.Fatal("drainDone was not closed after worker join")
		}
		require.Less(t, elapsed, 1*time.Second, "must join promptly after cancellation without arbitrary sleep")
	})
}

// TestOracleCorrelation_PendingQueuePointerClearing verifies that pendingQueue.pop clears
// the vacated slot pointer to prevent memory retention, reuses its fixed backing storage,
// and that clear() nils out all retained references.
func TestOracleCorrelation_PendingQueuePointerClearing(t *testing.T) {
	q := newPendingQueue(4)
	require.Equal(t, 4, q.capacity())
	require.Equal(t, 0, q.len())
	require.False(t, q.isFull())

	op1 := &pendingOp{reqID: 1, rowID: 10, sentVal: 100}
	op2 := &pendingOp{reqID: 2, rowID: 20, sentVal: 200}
	require.True(t, q.push(op1))
	require.True(t, q.push(op2))
	require.Equal(t, 2, q.len())

	// Pop op1: op1 must be returned and its slot in q.ops cleared
	head := q.pop()
	require.Equal(t, op1, head)
	require.Equal(t, 1, q.len())
	require.Nil(t, q.ops[0])
	require.Equal(t, op2, q.ops[q.head])

	// Pop op2: queue becomes empty and both occupied slots are cleared
	head2 := q.pop()
	require.Equal(t, op2, head2)
	require.Equal(t, 0, q.len())

	// Push 4 items to fill to cap
	for i := 0; i < 4; i++ {
		require.True(t, q.push(&pendingOp{reqID: uint64(i + 10)}))
	}
	require.True(t, q.isFull())
	require.False(t, q.push(&pendingOp{reqID: 99}), "pushing beyond cap must fail")

	// Clear queue: all references in underlying slice must be nil
	q.clear()
	require.Equal(t, 0, q.len())
	require.Nil(t, q.ops)
}

// TestOracleCorrelation_PendingCapBackpressureAndLateReplyDrain verifies that when
// the per-worker/per-actor pending cap is reached, incoming requests cannot be admitted
// until late replies are drained to free up capacity, ensuring progress without forgotten operations.
func TestOracleCorrelation_PendingCapBackpressureAndLateReplyDrain(t *testing.T) {
	capSize := 2
	queue := newPendingQueue(capSize)
	actState := newPartitionActorState()
	res := &openLoopResult{}

	// Enqueue 2 operations to saturate cap
	op1 := &pendingOp{reqID: 1, op: opPut, rowID: 5, sentVal: 100, writeSeq: 1}
	op2 := &pendingOp{reqID: 2, op: opPut, rowID: 6, sentVal: 200, writeSeq: 1}
	require.True(t, queue.push(op1))
	require.True(t, queue.push(op2))
	require.True(t, queue.isFull())

	// Create harness client receiver and worker client with dedicated replyCh
	recv := newHarnessClientReceiver()
	replyCh := make(chan *relayapi.Message, 1024)
	clientPID := pid.PID{Node: "local", Host: "client", UniqID: "test-client"}
	recv.registerClient("test-client", replyCh)
	defer recv.unregisterClient("test-client")

	client := &harnessWorkerClient{
		pid:     clientPID,
		replyCh: replyCh,
		uniqID:  "test-client",
	}

	// Case 1: Cap is full. Cannot admit op3 before freeing space.
	op3 := &pendingOp{reqID: 3, op: opPut, rowID: 7, sentVal: 300, writeSeq: 1}
	require.False(t, queue.push(op3), "cannot push to queue when cap is reached")

	// Case 2: Deliver late reply for op1 into client.replyCh
	replyMsg1 := relayapi.AcquireMessage()
	replyMsg1.Topic = "result"
	replyMsg1.Payloads = append(replyMsg1.Payloads, parseToPayload("updated:5:100"))
	pkg1 := relayapi.NewMessagePackage(pid.PID{}, clientPID, replyMsg1)
	require.NoError(t, recv.Send(pkg1))

	// Drain replies using drainWorkerReplies: op1 is popped, validated, and state updated
	drained := drainWorkerReplies(client, queue, actState, res, 0, 0, t)
	require.True(t, drained)
	require.Equal(t, 1, queue.len(), "queue length must decrease to 1 after draining late reply")
	require.False(t, queue.isFull(), "queue must no longer be full")
	require.Equal(t, int64(100), actState.acked[5], "late reply must update reference oracle state")

	// Case 3: Now op3 can be admitted and pushed to queue! Progress is made!
	require.True(t, queue.push(op3))
	require.Equal(t, 2, queue.len())
	require.True(t, queue.isFull())

	// Deliver late reply for op2 and reply for op3
	replyMsg2 := relayapi.AcquireMessage()
	replyMsg2.Topic = "result"
	replyMsg2.Payloads = append(replyMsg2.Payloads, parseToPayload("updated:6:200"))
	pkg2 := relayapi.NewMessagePackage(pid.PID{}, clientPID, replyMsg2)
	require.NoError(t, recv.Send(pkg2))

	replyMsg3 := relayapi.AcquireMessage()
	replyMsg3.Topic = "result"
	replyMsg3.Payloads = append(replyMsg3.Payloads, parseToPayload("updated:7:300"))
	pkg3 := relayapi.NewMessagePackage(pid.PID{}, clientPID, replyMsg3)
	require.NoError(t, recv.Send(pkg3))

	drained2 := drainWorkerReplies(client, queue, actState, res, 0, 0, t)
	require.True(t, drained2)
	require.Equal(t, 0, queue.len(), "all pending ops drained, no forgotten operations")
	require.Equal(t, int64(200), actState.acked[6])
	require.Equal(t, int64(300), actState.acked[7])
	require.Empty(t, res.semanticErrors)
}

// TestOracleCorrelation_FIFOClientReplyDropDetection verifies that client reply delivery
// channel has capacity 1024 and nonblocking drop semantics, and proves that any dropped
// reply increments receiver.dropped and would desynchronize FIFO pending correlation.
func TestOracleCorrelation_FIFOClientReplyDropDetection(t *testing.T) {
	recv := newHarnessClientReceiver()
	ch := make(chan *relayapi.Message, 1024)
	clientPID := pid.PID{Node: "local", Host: "client", UniqID: "drop-test"}
	recv.registerClient("drop-test", ch)
	defer recv.unregisterClient("drop-test")

	// Fill channel to exact buffer capacity (1024)
	for i := 0; i < 1024; i++ {
		msg := relayapi.AcquireMessage()
		msg.Topic = "result"
		msg.Payloads = append(msg.Payloads, parseToPayload(fmt.Sprintf("value:0:%d", i)))
		pkg := relayapi.NewMessagePackage(pid.PID{}, clientPID, msg)
		require.NoError(t, recv.Send(pkg))
	}
	require.Equal(t, 1024, len(ch))
	require.Zero(t, recv.dropped.Load(), "no drops within 1024 channel capacity")

	// Message 1025 overflows nonblocking channel select: dropped counter increments
	overflowMsg := relayapi.AcquireMessage()
	overflowMsg.Topic = "result"
	overflowMsg.Payloads = append(overflowMsg.Payloads, parseToPayload("value:0:1025"))
	overflowPkg := relayapi.NewMessagePackage(pid.PID{}, clientPID, overflowMsg)
	require.NoError(t, recv.Send(overflowPkg))

	require.Equal(t, uint64(1), recv.dropped.Load(), "overflow message must increment receiver.dropped")

	// Verify that dropping a reply desynchronizes FIFO matching:
	// Suppose pending queue expected op0 (value 0) and op1 (value 1)
	queue := newPendingQueue(10)
	op0 := &pendingOp{reqID: 0, op: opGet, rowID: 0}
	op1 := &pendingOp{reqID: 1, op: opGet, rowID: 0}
	queue.push(op0)
	queue.push(op1)

	msg0 := <-ch
	head0 := queue.pop()
	require.Equal(t, uint64(0), head0.reqID)
	text0, err := parseResultText(msg0)
	require.NoError(t, err)
	require.Equal(t, "value:0:0", text0)

	// This demonstrates why require.Zero(t, cluster.receiver.dropped.Load()) is required
	// for harness sound execution.
}

// TestSQLiteActor_OpenLoop_MultiActorCorrelation verifies actual SQLite open loop execution
// across multiple concurrent actors with dedicated per-actor worker clients and queues.
func TestSQLiteActor_OpenLoop_MultiActorCorrelation(t *testing.T) {
	cfg := openLoopConfig{
		actors:        2,
		rowsPerActor:  500,
		workers:       2,
		queueCapacity: 64,
		targetRate:    100, // 100 req/s across 2 actors
		duration:      500 * time.Millisecond,
		reqTimeout:    2 * time.Second,
		drainBudget:   5 * time.Second,
		writeRatio:    25, // 75% GET, 25% PUT
		mailboxLimits: nil,
	}
	res := runOpenLoopLoad(t, cfg)
	require.Greater(t, res.counts.completed.Load(), uint64(0))
	require.Zero(t, res.counts.generatorDropped.Load(), "no generator drops under in-capacity load")
	require.Zero(t, res.counts.failed.Load(), "no failures under in-capacity load")
	require.Zero(t, res.counts.timedout.Load(), "no timeouts under in-capacity load")
	require.Zero(t, res.counts.failedExecution.Load(), "no semantic failures")
	require.Empty(t, res.semanticErrors, "must have zero semantic errors")
	require.Equal(t, res.counts.planned.Load(), res.counts.completed.Load())
}

func TestOracleCorrelation_PendingQueueReusesStorage(t *testing.T) {
	q := newPendingQueue(4)
	op := &pendingOp{reqID: 1}
	allocations := testing.AllocsPerRun(1000, func() {
		for range 100 {
			if !q.push(op) || q.pop() != op {
				panic("queue order lost")
			}
		}
	})
	require.Zero(t, allocations, "repeated queue use must reuse its backing storage")
}
