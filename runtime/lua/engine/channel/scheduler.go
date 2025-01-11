package channel

import (
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

type chanOp int

const (
	chanSend chanOp = iota
	chanReceive
	chanClose
)

// carries chan operation context over to the bufferedScheduler
type pendingOp struct {
	task     *engine.Task
	op       *chanOperation
	next     *pendingOp
	selectOp *selectOperation // If this op is part of a select
}

func (p *pendingOp) reset() {
	p.task = nil
	p.op = nil
	p.next = nil
	p.selectOp = nil
}

// chanOperation sent via yields to coordinate channel communication
type chanOperation struct {
	dir   chanOp
	ch    *Channel
	value lua.LValue
}

func (y *chanOperation) String() string {
	switch y.dir {
	case chanSend:
		return fmt.Sprintf("channel.send{value=%+v}", y.value)
	case chanReceive:
		return fmt.Sprintf("channel.receive")
	case chanClose:
		return fmt.Sprintf("channel.close")
	}
	return "unknown"
}

func (y *chanOperation) Type() lua.LValueType {
	return lua.LTUserData
}

// taskResult helps handle task completion and return values
type taskResult struct {
	tasks []*engine.Task
}

func newTaskResult() *taskResult {
	return &taskResult{
		tasks: make([]*engine.Task, 0, 2),
	}
}

// with returns current task result for chaining
func (tr *taskResult) with(task *engine.Task) *taskResult {
	tr.tasks = append(tr.tasks, task)
	return tr
}

// withSuccess returns taskResult with task marked as successful with values
func (tr *taskResult) withSuccess(task *engine.Task, values ...lua.LValue) *taskResult {
	task.Resumed = values
	return tr.with(task)
}

// withError returns taskResult with task marked as failed
func (tr *taskResult) withError(task *engine.Task, err error) *taskResult {
	task.RaiseError = err
	return tr.with(task)
}

// done returns completed tasks slice
func (tr *taskResult) done() []*engine.Task {
	return tr.tasks
}

type bufferedScheduler struct {
	ops *channelOps
}

func newScheduler() *bufferedScheduler {
	return &bufferedScheduler{
		ops: newChannelOps(),
	}
}

func (s *bufferedScheduler) handleTasks(tasks []*engine.Task) ([]*engine.Task, error) {
	var result []*engine.Task

	for _, task := range tasks {
		if len(task.Yielded) == 0 {
			result = append(result, task)
			continue
		}

		switch op := task.Yielded[0].(type) {
		case *chanOperation:
			processed := s.handleOperation(task, op)
			result = append(result, processed...)
		case *selectOperation:
			processed := s.handleSelect(task, op)
			result = append(result, processed...)
		default:
			result = append(result, task)
		}
	}

	return result, nil
}

func (s *bufferedScheduler) handleOperation(task *engine.Task, op *chanOperation) []*engine.Task {
	switch op.dir {
	case chanSend:
		return s.handleSend(task, op)
	case chanReceive:
		return s.handleReceive(task, op)
	case chanClose:
		return s.handleClose(task, op)
	}
	return nil
}

func (s *bufferedScheduler) handleSend(task *engine.Task, op *chanOperation) []*engine.Task {
	ch := op.ch
	if ch.closed {
		return newTaskResult().withError(task, errors.New("channel closed")).done()
	}

	// Try matching with a waiting receiver
	if receiverTask := s.ops.findReceiver(ch, op.value); receiverTask != nil {
		return newTaskResult().
			withSuccess(task, lua.LBool(true)).
			with(receiverTask).
			done()
	}

	// Try buffering
	if s.ops.trySend(ch, op.value) {
		return newTaskResult().withSuccess(task, lua.LBool(true)).done()
	}

	// Queue the send operation
	s.ops.queueOperation(task, op, nil)
	return nil
}

func (s *bufferedScheduler) handleReceive(task *engine.Task, op *chanOperation) []*engine.Task {
	ch := op.ch

	// Try buffered value first
	if value, ok := ch.receive(); ok {
		// Return buffered value regardless of closed state
		return newTaskResult().withSuccess(task, value, lua.LBool(true)).done()
	}

	// If closed and no buffered values left, return nil,false
	if ch.closed {
		return newTaskResult().withSuccess(task, lua.LNil, lua.LBool(false)).done()
	}

	// Try matching with a sender for non-named channels
	if !ch.IsNamed() {
		if value, senderTask := s.ops.findSender(ch); senderTask != nil {
			return newTaskResult().
				withSuccess(task, value, lua.LBool(true)).
				with(senderTask).
				done()
		}
	}

	// Queue the receive operation if channel is open
	s.ops.queueOperation(task, op, nil)
	return nil
}
func (s *bufferedScheduler) handleClose(task *engine.Task, op *chanOperation) []*engine.Task {
	ch := op.ch
	if ch.closed {
		return newTaskResult().withError(task, fmt.Errorf("channel already closed")).done()
	}

	ch.closed = true
	result := newTaskResult().withSuccess(task, lua.LBool(true))

	// Handle all senders - they get error
	for {
		if _, senderTask := s.ops.findSender(ch); senderTask == nil {
			break
		} else {
			result.withError(senderTask, fmt.Errorf("channel closed"))
		}
	}

	// Handle only waiting receivers - DO NOT drain buffer
	for {
		if recvTask := s.ops.findReceiver(ch, nil); recvTask != nil {
			// If there are buffered values, give those first
			if value, ok := ch.receive(); ok {
				result.withSuccess(recvTask, value, lua.LBool(true))
			} else {
				result.withSuccess(recvTask, lua.LNil, lua.LBool(false))
			}
		} else {
			break
		}
	}

	return result.done()
}

func (s *bufferedScheduler) handleSelect(task *engine.Task, op *selectOperation) []*engine.Task {
	// Try immediate operations first
	for _, sc := range op.cases {
		ch := sc.Channel()

		switch sc.dir {
		case chanSend:
			if ch.closed {
				return newTaskResult().
					withError(task, fmt.Errorf("channel closed")).
					done()
			}

			// Try matching with a receiver
			if recvTask := s.ops.findReceiver(ch, sc.value); recvTask != nil {
				return newTaskResult().
					withSuccess(task, op.caseResult(task.Thread(), ch, nil, true)).
					with(recvTask).
					done()
			}

			// Try buffered send
			if s.ops.trySend(ch, sc.value) {
				return newTaskResult().
					withSuccess(task, op.caseResult(task.Thread(), ch, nil, true)).
					done()
			}

		case chanReceive:
			// Try receive from buffer or matching with sender
			if value, ok := s.ops.tryReceive(ch); ok {
				return newTaskResult().
					withSuccess(task, op.caseResult(task.Thread(), ch, value, true)).
					done()
			}
			if ch.closed {
				return newTaskResult().
					withSuccess(task, op.caseResult(task.Thread(), ch, nil, false)).
					done()
			}
			if value, senderTask := s.ops.findSender(ch); senderTask != nil {
				return newTaskResult().
					withSuccess(task, op.caseResult(task.Thread(), ch, value, true)).
					with(senderTask).
					done()
			}
		}
	}

	if op.hasDefault {
		return newTaskResult().
			withSuccess(task, op.caseResult(task.Thread(), nil, nil, true)).
			done()
	}

	// Queue select operation
	for _, sc := range op.cases {
		s.ops.queueOperation(task, &chanOperation{
			dir:   sc.dir,
			ch:    sc.Channel(),
			value: sc.value,
		}, op)
	}

	return nil
}

func (s *bufferedScheduler) send(name string, value lua.LValue) ([]*engine.Task, error) {
	if task, err := s.ops.pushNamed(name, value); err != nil {
		return nil, err
	} else {
		return []*engine.Task{task}, nil
	}
}

func (s *bufferedScheduler) getOpenChannels() []string {
	return s.ops.getOpenChannels()
}

func (s *bufferedScheduler) close() {
	s.ops.cleanup()
}
