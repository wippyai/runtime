// SPDX-License-Identifier: MPL-2.0

// Package cdc provides the command dispatcher shared by all CDC drivers.
package cdc

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	cdcapi "github.com/wippyai/runtime/api/service/cdc"
	"go.uber.org/zap"
)

const defaultWorkers = 4

var (
	// ErrDispatcherNotStarted is returned to a command submitted before Start.
	ErrDispatcherNotStarted = errors.New("cdc dispatcher is not started")
	// ErrDispatcherStopping is returned to a command submitted during Stop.
	ErrDispatcherStopping = errors.New("cdc dispatcher is stopping")
	// ErrDispatcherStarted is returned when Start is called for an active run.
	ErrDispatcherStarted = errors.New("cdc dispatcher is already started")
	// ErrUnknownCommand identifies a command that was not registered by this dispatcher.
	ErrUnknownCommand = errors.New("unknown cdc dispatcher command")
	// ErrNoSourceStreamer indicates that CDC sources were not installed in the context.
	ErrNoSourceStreamer = errors.New("cdc source streamer not available")
	// ErrNilSource indicates a corrupt registry entry that claims to exist but
	// does not provide a source implementation.
	ErrNilSource = errors.New("cdc registry returned a nil source")
	// ErrNoRelayNode indicates that the process has no relay transport.
	ErrNoRelayNode = errors.New("cdc relay node not available")
	// ErrRelayNotCancellable indicates that a relay node cannot bind delivery
	// to the subscription lifecycle. Detaching an unowned Send goroutine would
	// leak it, so CDC refuses that delivery path instead.
	ErrRelayNotCancellable = errors.New("cdc relay node does not support cancellable delivery")
)

type dispatcherState uint8

const (
	stateNew dispatcherState = iota
	stateRunning
	stateStopping
	stateStopped
)

// Dispatcher routes CDC subscriptions from the process dispatcher to the
// configured source manager. The dispatcher owns subscription relays; a
// driver owns the source and its stream implementation.
type Dispatcher struct {
	ctx      context.Context
	log      *zap.Logger
	cancel   context.CancelFunc
	jobs     chan dispatchJob
	sessions map[uint64]*relaySession
	stopDone chan struct{}
	// admissionsDone is a per-run barrier. Stop cancels workers first, then
	// waits for handles that already passed the state check before workers
	// drain the queue. This closes the Handle/Stop admission race without
	// holding mu across a potentially blocking queue send.
	admissionsDone chan struct{}
	workersWG      sync.WaitGroup
	relaysWG       sync.WaitGroup
	admissions     int
	workers        int
	nextID         uint64
	mu             sync.Mutex
	state          dispatcherState
}

type dispatchJob struct {
	ctx      context.Context
	cmd      dispatcher.Command
	receiver dispatcher.ResultReceiver
	tag      uint64
}

// DispatcherOption configures a Dispatcher.
type DispatcherOption func(*Dispatcher)

// WithWorkers sets the number of command workers. Values less than one are
// ignored and leave the default (or previously configured) value unchanged.
func WithWorkers(n int) DispatcherOption {
	return func(d *Dispatcher) {
		if n > 0 {
			d.workers = n
		}
	}
}

// WithLogger sets the dispatcher logger.
func WithLogger(log *zap.Logger) DispatcherOption {
	return func(d *Dispatcher) {
		if log != nil {
			d.log = log
		}
	}
}

// NewDispatcher creates a CDC dispatcher.
func NewDispatcher(opts ...DispatcherOption) *Dispatcher {
	d := &Dispatcher{
		workers:  defaultWorkers,
		state:    stateNew,
		sessions: make(map[uint64]*relaySession),
		log:      zap.NewNop(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(d)
		}
	}
	return d
}

// Start starts the command workers. A dispatcher can be started again after a
// completed Stop, but cannot be started concurrently with an active run.
func (d *Dispatcher) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	d.mu.Lock()
	if d.state == stateRunning {
		d.mu.Unlock()
		return ErrDispatcherStarted
	}
	if d.state == stateStopping {
		d.mu.Unlock()
		return ErrDispatcherStopping
	}

	d.ctx, d.cancel = context.WithCancel(ctx)
	d.jobs = make(chan dispatchJob, d.workers*2)
	d.sessions = make(map[uint64]*relaySession)
	d.stopDone = make(chan struct{})
	d.admissionsDone = make(chan struct{})
	d.admissions = 0
	d.state = stateRunning
	for i := 0; i < d.workers; i++ {
		d.workersWG.Add(1)
	}
	runCtx := d.ctx
	stopDone := d.stopDone
	admissionsDone := d.admissionsDone
	d.mu.Unlock()

	for i := 0; i < d.workers; i++ {
		go d.worker(runCtx, admissionsDone)
	}
	go d.monitorContext(runCtx, stopDone)
	return nil
}

// Stop stops workers and all active relays. It is safe to call concurrently
// with Handle and can be called more than once. If ctx expires, cleanup
// continues in the background and a later Stop observes its final result.
func (d *Dispatcher) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	d.mu.Lock()
	switch d.state {
	case stateNew, stateStopped:
		d.mu.Unlock()
		return nil
	case stateStopping:
		done := d.stopDone
		d.mu.Unlock()
		return waitForStop(ctx, done)
	case stateRunning:
		d.state = stateStopping
		done := d.stopDone
		cancel := d.cancel
		d.closeAdmissionsLocked()
		sessions := make([]*relaySession, 0, len(d.sessions))
		for _, session := range d.sessions {
			sessions = append(sessions, session)
		}
		d.mu.Unlock()

		if cancel != nil {
			cancel()
		}
		for _, session := range sessions {
			session.stop()
		}
		go d.finishStop(done)
		return waitForStop(ctx, done)
	default:
		d.mu.Unlock()
		return nil
	}
}

// monitorContext turns cancellation of the context supplied to Start into a
// normal dispatcher stop. The stopDone identity prevents a stale monitor from
// stopping a later run after the dispatcher has been restarted.
func (d *Dispatcher) monitorContext(runCtx context.Context, stopDone chan struct{}) {
	<-runCtx.Done()

	d.mu.Lock()
	valid := d.state == stateRunning && d.stopDone == stopDone
	d.mu.Unlock()
	if valid {
		_ = d.Stop(context.Background())
	}
}

func (d *Dispatcher) finishStop(done chan struct{}) {
	d.workersWG.Wait()
	d.relaysWG.Wait()

	d.mu.Lock()
	if d.state == stateStopping && d.stopDone == done {
		d.state = stateStopped
		d.ctx = nil
		d.cancel = nil
		d.jobs = nil
		close(done)
	}
	d.mu.Unlock()
}

func waitForStop(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Dispatcher) worker(ctx context.Context, admissionsDone <-chan struct{}) {
	defer d.workersWG.Done()

	for {
		select {
		case job := <-d.jobs:
			if ctx.Err() != nil {
				complete(job.receiver, job.tag, nil, ErrDispatcherStopping)
				continue
			}
			d.execute(ctx, job)
		case <-ctx.Done():
			// A Handle may have passed the running-state check but not yet
			// enqueued its job. Wait for those admissions before draining;
			// otherwise a send racing cancellation could land in a queue with
			// no worker left to complete it.
			<-admissionsDone
			d.drainJobs()
			return
		}
	}
}

func (d *Dispatcher) closeAdmissionsLocked() {
	if d.admissions == 0 && d.admissionsDone != nil {
		select {
		case <-d.admissionsDone:
		default:
			close(d.admissionsDone)
		}
	}
}

func (d *Dispatcher) endAdmission(done chan struct{}) {
	d.mu.Lock()
	d.admissions--
	if d.admissions == 0 && d.state != stateRunning {
		select {
		case <-done:
		default:
			close(done)
		}
	}
	d.mu.Unlock()
}

// drainJobs completes commands accepted before cancellation. Jobs are never
// dropped silently, which prevents a process yield from remaining pending
// when the dispatcher is stopped.
func (d *Dispatcher) drainJobs() {
	for {
		select {
		case job := <-d.jobs:
			complete(job.receiver, job.tag, nil, ErrDispatcherStopping)
		default:
			return
		}
	}
}

func (d *Dispatcher) execute(dispatchCtx context.Context, job dispatchJob) {
	switch cmd := job.cmd.(type) {
	case cdcapi.SubscribeCmd:
		d.executeSubscribe(dispatchCtx, job.ctx, cmd, job.tag, job.receiver)
	case *cdcapi.SubscribeCmd:
		if cmd == nil {
			complete(job.receiver, job.tag, nil, fmt.Errorf("%w: nil subscribe command", ErrUnknownCommand))
			return
		}
		d.executeSubscribe(dispatchCtx, job.ctx, *cmd, job.tag, job.receiver)
	default:
		complete(job.receiver, job.tag, nil, fmt.Errorf("%w: %T", ErrUnknownCommand, job.cmd))
	}
}

func (d *Dispatcher) executeSubscribe(dispatchCtx, requestCtx context.Context, cmd cdcapi.SubscribeCmd, tag uint64, receiver dispatcher.ResultReceiver) {
	if err := cmd.Options.Validate(); err != nil {
		complete(receiver, tag, nil, err)
		return
	}
	// Keep source, relay, and process admission on one finite item limit.
	// Lua normalizes this before yielding; direct Go callers get the same
	// bounded default here.
	cmd.Options.Buffer = cmd.Options.EffectiveMaxStreamItems()
	ctx, cancelContext := linkedContext(dispatchCtx, requestCtx)
	node := relay.GetNode(ctx)
	if node == nil {
		cancelContext()
		complete(receiver, tag, nil, ErrNoRelayNode)
		return
	}

	stream, err := d.openStream(ctx, cmd)
	if err != nil {
		cancelContext()
		if dispatchCtx != nil && dispatchCtx.Err() != nil {
			err = ErrDispatcherStopping
		}
		complete(receiver, tag, nil, err)
		return
	}
	if stream == nil {
		cancelContext()
		complete(receiver, tag, nil, errors.New("cdc source returned a nil stream"))
		return
	}

	loopCtx, cancelLoop := context.WithCancel(ctx)
	session := &relaySession{
		maxBytes: cmd.Options.EffectiveMaxBytes(),
		maxItems: cmd.Options.EffectiveMaxStreamItems(),
		cancel: func() {
			cancelLoop()
			cancelContext()
		},
		close: stream.Close,
	}
	if !d.addSession(session) {
		session.stop()
		complete(receiver, tag, nil, ErrDispatcherStopping)
		return
	}

	changes := stream.Changes()
	go d.relay(loopCtx, session, changes, stream, node, cmd.PID, cmd.Topic, cmd.Source)

	complete(receiver, tag, cdcapi.Subscription{
		Source: cmd.Source,
		Topic:  cmd.Topic,
		Stop:   session.stop,
	}, nil)
}

// openStream resolves the canonical system registry first. The legacy
// SourceStreamer path is retained temporarily for callers that predate the
// driver-neutral registry; boot uses the registry path for every driver.
func (d *Dispatcher) openStream(ctx context.Context, cmd cdcapi.SubscribeCmd) (changeStream, error) {
	if reg := cdcapi.GetRegistry(ctx); reg != nil {
		source, ok := reg.Get(registry.ParseID(cmd.Source))
		if ok {
			if source == nil {
				return nil, fmt.Errorf("%w: %s", ErrNilSource, cmd.Source)
			}
			return source.Subscribe(ctx, cmd.Options)
		}

		// A registry miss can still be served by a pre-registry caller's
		// streamer. A present registry entry, including a corrupt nil entry,
		// remains authoritative so legacy aliases cannot shadow canonical IDs.
		if streamer := cdcapi.GetSourceStreamer(ctx); streamer != nil {
			stream, _, err := streamer.Stream(ctx, cmd.Source, cmd.Options)
			return stream, err
		}
		return nil, fmt.Errorf("%w: %s", cdcapi.ErrSourceNotFound, cmd.Source)
	}

	streamer := cdcapi.GetSourceStreamer(ctx)
	if streamer == nil {
		return nil, ErrNoSourceStreamer
	}
	stream, _, err := streamer.Stream(ctx, cmd.Source, cmd.Options)
	return stream, err
}

// linkedContext preserves the request context's values (including relay
// routing) while making dispatcher shutdown a second cancellation parent. The
// returned cleanup must be held by the relay session until the stream ends.
func linkedContext(dispatchCtx, requestCtx context.Context) (context.Context, context.CancelFunc) {
	if requestCtx == nil {
		requestCtx = context.Background()
	}
	if dispatchCtx == nil {
		return context.WithCancel(requestCtx)
	}
	ctx, cancel := context.WithCancel(requestCtx)
	stopPropagation := context.AfterFunc(dispatchCtx, cancel)
	return ctx, func() {
		stopPropagation()
		cancel()
	}
}

func (d *Dispatcher) addSession(session *relaySession) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.state != stateRunning {
		return false
	}
	d.nextID++
	session.id = d.nextID
	d.sessions[session.id] = session
	d.relaysWG.Add(1)
	return true
}

func (d *Dispatcher) relay(ctx context.Context, session *relaySession, changes <-chan cdcapi.Change, stream changeStream, node relay.Node, target pid.PID, topic, source string) {
	defer func() {
		// Closing a naturally exhausted stream is still the dispatcher's
		// ownership responsibility. stop is idempotent and suppresses any
		// duplicate terminal caused by the close.
		session.stop()
		d.relayDone(session.id)
		d.relaysWG.Done()
	}()

	for {
		select {
		case change, ok := <-changes:
			if !ok {
				if err := streamError(stream); err != nil {
					d.sendTerminal(ctx, node, target, topic, session.maxItems, session.maxBytes, err)
				} else {
					d.sendTerminal(ctx, node, target, topic, session.maxItems, session.maxBytes, nil)
				}
				return
			}

			pkg := relay.NewPackage(pid.Zero(), target, topic, payload.New(change))
			pkg.Messages[0].PayloadBytes = cdcapi.EstimateChangeBytes(change)
			pkg.Messages[0].MaxBytes = session.maxBytes
			pkg.Messages[0].MaxItems = session.maxItems
			if err := sendRelay(ctx, node, pkg); err != nil {
				d.log.Debug("failed to relay cdc change",
					zap.String("source", source),
					zap.Error(err))
				// A failed relay cannot make progress. Close the source stream
				// and cancel this relay so it cannot retain a worker or source.
				session.stop()
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (d *Dispatcher) relayDone(id uint64) {
	d.mu.Lock()
	delete(d.sessions, id)
	d.mu.Unlock()
}

func (d *Dispatcher) sendTerminal(ctx context.Context, node relay.Node, target pid.PID, topic string, maxItems int, maxBytes int64, err error) {
	var terminal payload.Payloads
	if err != nil {
		terminal = append(terminal, payload.NewError(err))
	}
	terminal = append(terminal, payload.NewTerminal())
	pkg := relay.NewPackage(pid.Zero(), target, topic, terminal...)
	pkg.Messages[0].MaxItems = maxItems
	pkg.Messages[0].MaxBytes = maxBytes
	if sendErr := sendRelay(ctx, node, pkg); sendErr != nil {
		d.log.Debug("failed to send cdc terminal",
			zap.String("topic", topic),
			zap.Error(sendErr))
	}
}

// sendRelay uses the relay-owned cancellation contract. Starting an
// untracked goroutine around Receiver.Send would make Stop return while that
// goroutine retained the stream, node, and process context indefinitely; a
// node without the capability therefore fails the delivery explicitly.
func sendRelay(ctx context.Context, node relay.Node, pkg *relay.Package) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	sender, ok := node.(relay.ContextSender)
	if !ok {
		return ErrRelayNotCancellable
	}
	return sender.SendContext(ctx, pkg)
}

// streamError is an optional extension implemented by streams that can
// report a typed terminal error after their change channel closes. Keeping it
// optional preserves compatibility with the original stream interface while
// allowing all drivers to expose terminal failures consistently.
func streamError(stream changeStream) error {
	if s, ok := stream.(interface{ Err() error }); ok {
		return s.Err()
	}
	return nil
}

type changeStream interface {
	Changes() <-chan cdcapi.Change
	Close()
}

func complete(receiver dispatcher.ResultReceiver, tag uint64, data any, err error) {
	if receiver != nil {
		receiver.CompleteYield(tag, data, err)
	}
}

type relaySession struct {
	cancel   context.CancelFunc
	close    func()
	maxBytes int64
	maxItems int
	id       uint64
	once     sync.Once
}

func (s *relaySession) stop() {
	s.once.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		if s.close != nil {
			s.close()
		}
	})
}

// Handle queues a command for execution by the dispatcher worker pool.
func (d *Dispatcher) Handle(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	if ctx == nil {
		ctx = context.Background()
	}

	d.mu.Lock()
	if d.state != stateRunning {
		err := ErrDispatcherNotStarted
		if d.state == stateStopping {
			err = ErrDispatcherStopping
		}
		d.mu.Unlock()
		complete(receiver, tag, nil, err)
		return nil
	}
	jobs := d.jobs
	runCtx := d.ctx
	d.admissions++
	admissionsDone := d.admissionsDone
	d.mu.Unlock()

	job := dispatchJob{ctx: ctx, cmd: cmd, tag: tag, receiver: receiver}
	enqueued := false
	select {
	case jobs <- job:
		enqueued = true
	case <-runCtx.Done():
		// The admission barrier keeps workers from draining until this
		// in-flight send has resolved, even if both cases are ready.
	case <-ctx.Done():
		// Complete below after releasing the admission barrier.
	}
	d.endAdmission(admissionsDone)
	if !enqueued {
		err := ErrDispatcherStopping
		if runCtx.Err() == nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		complete(receiver, tag, nil, err)
	}
	return nil
}

// RegisterAll registers all CDC command handlers with the process dispatcher.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	if register != nil {
		register(cdcapi.Subscribe, dispatcher.HandlerFunc(d.Handle))
	}
}
