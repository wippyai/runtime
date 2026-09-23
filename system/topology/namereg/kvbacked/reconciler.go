// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/system/topology/namereg/global"
	"go.uber.org/zap"
)

type reconcilerLifecycle struct {
	ctx    context.Context
	cancel context.CancelFunc
	watch  atomic.Pointer[reconcilerWatch]
	failed atomic.Bool
}

type reconcilerWatch struct{ kvapi.Watcher }

// strongReconcileWorkers bounds concurrent Raft-backed actions. The owner
// retains one generation per live name independently of worker capacity.
const strongReconcileWorkers = 4

type reconcileSlot struct {
	queueElem *list.Element
	attemptID string
	dirty     bool
	inFlight  bool
}

type reconcileWork struct {
	slot *reconcileSlot
	name string
	scan bool
}

type reconcileReport struct {
	slot      *reconcileSlot
	scanErr   error
	err       error
	name      string
	attemptID string
	retryAt   int64
}

// reconcilerOwner is the sole owner of ordered watch events and the
// per-name coalescing state. Blocking Strong actions run in the fixed worker
// pool and report completion back to this loop.
type reconcilerOwner struct {
	svc           *Service
	lifecycle     *reconcilerLifecycle
	ctx           context.Context
	actions       chan reconcileWork
	done          chan reconcileReport
	wake          chan struct{}
	slots         map[string]*reconcileSlot
	started       chan struct{}
	ready         list.List
	mu            sync.Mutex
	scanRequested bool
	scanInFlight  bool
}

func newReconcilerOwner(s *Service, run *reconcilerLifecycle) *reconcilerOwner {
	return &reconcilerOwner{
		svc:       s,
		lifecycle: run,
		ctx:       run.ctx,
		actions:   make(chan reconcileWork, strongReconcileWorkers),
		done:      make(chan reconcileReport, strongReconcileWorkers),
		wake:      make(chan struct{}, 1),
		slots:     make(map[string]*reconcileSlot),
		started:   make(chan struct{}),
	}
}

// mark revisits a currently tracked name; an untagged notification cannot
// create a new obligation or resurrect an already retired generation.
func (o *reconcilerOwner) mark(name string) {
	o.markAttempt(name, "")
}

func (o *reconcilerOwner) markAttempt(name, attemptID string) {
	o.mu.Lock()
	o.markAttemptLocked(name, attemptID)
	o.mu.Unlock()
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

func (o *reconcilerOwner) markAttemptLocked(name, attemptID string) {
	slot := o.slots[name]
	if slot == nil && attemptID == "" {
		return
	}
	if slot == nil || (attemptID != "" && slot.attemptID != attemptID) {
		// Replacement retires the previous generation. A report for it may
		// still arrive, but pointer identity prevents it from mutating state.
		if slot != nil && slot.queueElem != nil {
			o.ready.Remove(slot.queueElem)
		}
		slot = &reconcileSlot{attemptID: attemptID}
		o.slots[name] = slot
		// A previous generation's timer must not suppress this attempt's
		// deadline. Timer callbacks only enqueue their own live generation.
		o.svc.strong.stopTimer(name)
	}
	slot.dirty = true
	if slot.queueElem == nil && !slot.inFlight {
		slot.queueElem = o.ready.PushBack(reconcileWork{name: name, slot: slot})
	}
}

// Recovery scans run concurrently with ordered watch delivery. Validate the
// scanned generation against the current KV snapshot while holding the owner
// lock so a delayed scan cannot replace a newer generation observed by watch.
func (o *reconcilerOwner) markScannedAttempt(name, attemptID string) error {
	o.mu.Lock()
	entry, err := o.svc.engine.Get(pendingKey(name))
	if err == nil {
		var header pendingHeader
		header, err = decodePending(entry.Value)
		if err == nil {
			err = validateNamingRecord(entry.Key, pendingPrefix, header.Name, header.PID)
		}
		if err == nil && header.Name == name && header.AttemptID == attemptID {
			o.markAttemptLocked(name, attemptID)
		}
	}
	o.mu.Unlock()
	if err != nil && !errors.Is(err, kvapi.ErrKeyNotFound) {
		return err
	}
	select {
	case o.wake <- struct{}{}:
	default:
	}
	return nil
}

// retryAttempt is a hint from a timer for an already live generation. A timer
// fired after DELETE or replacement cannot create a new owner obligation.
func (o *reconcilerOwner) retryAttempt(name, attemptID string) {
	o.mu.Lock()
	slot := o.slots[name]
	if slot == nil || slot.attemptID != attemptID {
		o.mu.Unlock()
		return
	}
	slot.dirty = true
	if slot.queueElem == nil && !slot.inFlight {
		slot.queueElem = o.ready.PushBack(reconcileWork{name: name, slot: slot})
	}
	o.mu.Unlock()
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

func (o *reconcilerOwner) retire(name, attemptID string) {
	o.mu.Lock()
	if slot := o.slots[name]; slot != nil && (attemptID == "" || slot.attemptID == attemptID) {
		if slot.queueElem != nil {
			o.ready.Remove(slot.queueElem)
		}
		delete(o.slots, name)
	}
	o.mu.Unlock()
}

func (o *reconcilerOwner) requestScan() {
	o.mu.Lock()
	o.scanRequested = true
	o.mu.Unlock()
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

func (o *reconcilerOwner) dispatch() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.scanRequested && !o.scanInFlight {
		select {
		case o.actions <- reconcileWork{scan: true}:
			o.scanRequested = false
			o.scanInFlight = true
		default:
		}
	}
	for front := o.ready.Front(); front != nil; front = o.ready.Front() {
		work := front.Value.(reconcileWork)
		slot := o.slots[work.name]
		if slot != work.slot || !slot.dirty || slot.inFlight {
			o.ready.Remove(front)
			if slot == work.slot {
				slot.queueElem = nil
			}
			continue
		}
		select {
		case o.actions <- work:
			o.ready.Remove(front)
			slot.dirty = false
			slot.inFlight = true
			slot.queueElem = nil
		default:
			return
		}
	}
}

func (o *reconcilerOwner) worker() {
	for {
		select {
		case <-o.ctx.Done():
			return
		case work := <-o.actions:
			report := reconcileReport{name: work.name}
			if work.scan {
				report.scanErr = o.svc.strong.reconcileAllPending()
			} else {
				report = o.svc.strong.reconcileForRun(work.name, o.lifecycle)
				report.slot = work.slot
			}
			select {
			case o.done <- report:
			case <-o.ctx.Done():
				return
			}
		}
	}
}

func (o *reconcilerOwner) actionDone(report reconcileReport) {
	if o.svc.reconciler.Load() != o.lifecycle || o.ctx.Err() != nil {
		return
	}
	if report.err != nil {
		o.svc.failReconciler(o.lifecycle, report.err)
		o.svc.logger.Error("strong reconciliation observation failed", zap.Error(report.err))
		return
	}
	if report.slot == nil {
		o.mu.Lock()
		o.scanInFlight = false
		o.mu.Unlock()
		if report.scanErr != nil {
			o.svc.logger.Error("strong recovery scan failed; closing naming admission", zap.Error(report.scanErr))
			o.svc.failReconciler(o.lifecycle, report.scanErr)
		}
		return
	}
	name := report.name
	o.mu.Lock()
	slot := o.slots[name]
	if slot == nil || slot != report.slot || !slot.inFlight {
		o.mu.Unlock()
		return
	}
	applyRetry := report.retryAt != 0 && (report.attemptID == "" || slot.attemptID == report.attemptID)
	slot.inFlight = false
	if !slot.dirty && report.attemptID == "" && report.retryAt == 0 {
		delete(o.slots, name)
	} else if slot.dirty {
		slot.queueElem = o.ready.PushBack(reconcileWork{name: name, slot: slot})
	}
	if applyRetry {
		o.svc.strong.armTimerAttempt(name, slot.attemptID, report.retryAt)
	}
	o.mu.Unlock()
}

func (o *reconcilerOwner) run(w kvapi.Watcher, run *reconcilerLifecycle) {
	for i := 0; i < strongReconcileWorkers; i++ {
		go o.worker()
	}
	close(o.started)
	var leadership raftapi.Leadership
	var fallback <-chan time.Time
	lastLeader := o.svc.leaderFn()
	if observe := o.svc.strong.observeLeadership; observe != nil {
		leadership = observe()
	} else {
		// Standalone registry instances can supply only IsLeader. Production
		// Raft supplies a revisioned observation instead of polling.
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		fallback = ticker.C
	}
	defer func() {
		o.svc.ready.Store(false)
		run.cancel()
		_ = w.Close()
		if o.svc.dissem != nil {
			o.svc.dissem.Stop()
		}
	}()
	for {
		o.dispatch()
		select {
		case <-o.ctx.Done():
			return
		case <-w.Done():
			return
		case work := <-o.done:
			o.actionDone(work)
		case <-o.wake:
		case <-leadership.Changed:
			leadership = o.svc.strong.observeLeadership()
			if leadership.State == raftapi.Leader && o.svc.leaderFn() {
				o.requestScan()
			}
		case <-fallback:
			leader := o.svc.leaderFn()
			if leader && !lastLeader {
				o.requestScan()
			}
			lastLeader = leader
		case ev, ok := <-w.Events():
			if !ok {
				return
			}
			select {
			case <-w.Done():
				return
			default:
			}
			if err := o.svc.handleWatchEvent(ev); err != nil {
				o.svc.failReconciler(run, err)
				o.svc.logger.Error("registry synchronization failed", zap.Error(err))
				return
			}
		}
	}
}

// StartReconciler drives the registry off the kv watch stream: active-binding
// changes feed the dissem cache (so non-members resolve names), and Strong
// pending/ack/reject changes advance the Strong state machine. No-op when
// neither dissem nor Strong is configured. The watcher stops when ctx ends.
// Successful startup owns this Service for its lifetime: restarting requires a
// new Service (including fresh dissemination state). Failed startup may retry.
func (s *Service) StartReconciler(ctx context.Context) (err error) {
	if s.strong == nil && s.dissem == nil {
		return nil
	}
	if s.strong != nil && s.nonMember != nil && s.nonMember() {
		return fmt.Errorf("naming participant without a local replica requires an authority feed")
	}
	if s.strong != nil && (s.localRead == nil || s.localScan == nil) {
		return fmt.Errorf("strong registry requires coherent local KV snapshots")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	run := &reconcilerLifecycle{ctx: ctx, cancel: cancel}
	var owner *reconcilerOwner
	s.reconcilerMu.Lock()
	installed := s.reconciler.CompareAndSwap(nil, run)
	s.reconcilerMu.Unlock()
	if !installed {
		cancel()
		return fmt.Errorf("registry reconciler already started; use a new service after shutdown")
	}
	defer func() {
		if err != nil {
			if owner != nil && s.strong != nil {
				s.strong.owner.CompareAndSwap(owner, nil)
			}
			s.reconcilerMu.Lock()
			cancel()
			s.reconciler.CompareAndSwap(run, nil)
			s.reconcilerMu.Unlock()
		}
	}()
	if s.strong != nil {
		if err := s.strong.enroll(ctx); err != nil {
			return err
		}
	}
	w, err := s.engine.Watch(ctx, registryPrefix)
	if err != nil {
		cancel()
		return err
	}
	run.watch.Store(&reconcilerWatch{Watcher: w})
	if s.strong != nil {
		owner = newReconcilerOwner(s, run)
		s.strong.owner.Store(owner)
	}
	// The delivery worker may be blocked in a snapshot read. Watch validity
	// must close admission independently of that worker's next receive.
	go func() {
		select {
		case <-w.Done():
			watchErr := w.Err()
			if watchErr == nil {
				watchErr = kvapi.ErrWatchClosed
			}
			s.failReconciler(run, watchErr)
		case <-ctx.Done():
		}
		if watchErr := w.Err(); errors.Is(watchErr, kvapi.ErrWatchOverflow) || errors.Is(watchErr, kvapi.ErrWatchReset) {
			s.logger.Error("registry watch invalidated; naming reconciliation stopped; restart required", zap.Error(watchErr))
		}
	}()
	if err := s.seed(); err != nil {
		cancel()
		_ = w.Close()
		return err
	}
	if err := ctx.Err(); err != nil {
		cancel()
		_ = w.Close()
		return err
	}
	select {
	case <-w.Done():
		cancel()
		_ = w.Close()
		return fmt.Errorf("registry watch invalid during seed: %w", w.Err())
	default:
	}
	if s.dissem != nil {
		go s.dissem.RunGC()
	}
	if s.strong != nil {
		go owner.run(w, run)
		<-owner.started
		// The owner is consuming ordered events before admission opens; startup
		// actions are already coalesced into its bounded work state.
		s.ready.Store(true)
	} else {
		s.ready.Store(true)
		go func() {
			defer func() {
				s.ready.Store(false)
				cancel()
				_ = w.Close()
				if s.dissem != nil {
					s.dissem.Stop()
				}
			}()
			for {
				select {
				case <-ctx.Done():
					return
				case <-w.Done():
					return
				case ev, ok := <-w.Events():
					if !ok {
						return
					}
					select {
					case <-w.Done():
						return
					default:
					}
					if err := s.handleWatchEvent(ev); err != nil {
						s.failReconciler(run, err)
						s.logger.Error("registry synchronization failed", zap.Error(err))
						return
					}
				}
			}
		}()
	}
	return nil
}

// failReconciler closes admission on a required observation failure. An old
// lifecycle cannot close readiness belonging to a replacement startup.
func (s *Service) failReconciler(run *reconcilerLifecycle, err error) {
	if err == nil || run == nil {
		return
	}
	s.reconcilerMu.Lock()
	defer s.reconcilerMu.Unlock()
	if s.reconciler.Load() != run || run.ctx.Err() != nil || !run.failed.CompareAndSwap(false, true) {
		return
	}
	s.ready.Store(false)
	run.cancel()
}

// seed primes local state from the current kv snapshot: dissem cache from active
// bindings, and the Strong machine from in-flight pending reservations.
func (s *Service) seed() error {
	var recordErr error
	participantSeen := false
	consume := func(e kvapi.Entry, observed uint64) bool {
		switch {
		case e.Key == participantsKey && s.strong != nil:
			roster, err := decodeParticipants(e.Value)
			if err != nil {
				recordErr = fmt.Errorf("registry record %q: %w", e.Key, err)
				return false
			}
			if !roster.hasActivation(s.selfNode, s.strong.activation) {
				recordErr = fmt.Errorf("naming activation changed before seed completed")
				return false
			}
			participantSeen = true
		case strings.HasPrefix(e.Key, pendingPrefix) && s.strong != nil:
			header, err := decodePending(e.Value)
			if err != nil {
				recordErr = fmt.Errorf("registry record %q: %w", e.Key, err)
				return false
			}
			if recordErr = validateNamingRecord(e.Key, pendingPrefix, header.Name, header.PID); recordErr != nil {
				return false
			}
			pendingPID, err := pid.ParsePID(header.PID)
			if err != nil {
				recordErr = err
				return false
			}
			s.strong.latchAt(header.Name, header.AttemptID, pendingPID, e.Epoch, observed)
			if owner := s.strong.owner.Load(); owner != nil {
				owner.markAttempt(header.Name, header.AttemptID)
			}
		case strings.HasPrefix(e.Key, activePrefix):
			active, err := decodeActive(e.Value)
			if err != nil {
				recordErr = fmt.Errorf("registry record %q: %w", e.Key, err)
				return false
			}
			if recordErr = validateNamingRecord(e.Key, activePrefix, active.Name, active.PID); recordErr != nil {
				return false
			}
			name := strings.TrimPrefix(e.Key, activePrefix)
			if s.dissem != nil {
				s.translateActive(name, e.Value, e.Epoch, false)
			}
			if s.strong != nil && active.Strong {
				owner, err := pid.ParsePID(active.PID)
				if err == nil {
					s.strong.onActive(name, active.AttemptID, e.Epoch, observed, owner)
				}
			}
		}
		return recordErr == nil
	}
	var err error
	if s.localScan != nil {
		err = s.localScan.ScanLocalSnapshot(registryPrefix, consume)
	} else {
		err = s.engine.Scan(registryPrefix, func(e kvapi.Entry) bool { return consume(e, 0) })
	}
	if err != nil {
		return err
	}
	if s.strong != nil && recordErr == nil && !participantSeen {
		return fmt.Errorf("naming participant activation missing from seed")
	}
	return recordErr
}

func validateNamingRecord(key, prefix, name, owner string) error {
	if key != prefix+name {
		return fmt.Errorf("registry record %q: name mismatch", key)
	}
	if _, err := pid.ParsePID(owner); err != nil {
		return fmt.Errorf("registry record %q: invalid owner: %w", key, err)
	}
	return nil
}

func (s *Service) handleWatchEvent(ev kvapi.WatchEvent) error {
	key := ""
	switch {
	case ev.Current != nil:
		key = ev.Current.Key
	case ev.Previous != nil:
		key = ev.Previous.Key
	}
	// Registry keys are never lease-bound, so a WatchExpired here is a design
	// violation. It is still handled safely below (as a delete); surface it.
	if ev.Type == kvapi.WatchExpired && strings.HasPrefix(key, registryPrefix) {
		s.logger.Debug("registry key expired via lease (unexpected)", zap.String("key", key))
	}
	switch {
	case key == participantsKey && s.strong != nil:
		if ev.Current == nil {
			return fmt.Errorf("naming participant roster was removed")
		}
		roster, err := decodeParticipants(ev.Current.Value)
		if err != nil {
			return fmt.Errorf("registry record %q: %w", key, err)
		}
		if !roster.hasActivation(s.selfNode, s.strong.activation) {
			return fmt.Errorf("naming participant activation was superseded")
		}
	case strings.HasPrefix(key, activePrefix):
		name := strings.TrimPrefix(key, activePrefix)
		if ev.Current != nil {
			av, err := decodeActive(ev.Current.Value)
			if err != nil {
				return fmt.Errorf("registry record %q: %w", key, err)
			}
			if err := validateNamingRecord(key, activePrefix, av.Name, av.PID); err != nil {
				return err
			}
			// The complete snapshot may already contain a later binding (or no
			// binding) by the time this event is handled. Deliver the committed
			// attempt's success from the ordered operation payload itself.
			if s.strong != nil {
				if av.Strong {
					owner, err := pid.ParsePID(av.PID)
					if err != nil {
						return fmt.Errorf("registry record %q: %w", key, err)
					}
					release := s.strong.admission.Acquire(name)
					s.strong.recordActive(name, av.AttemptID, ev.Index, ev.Revision, owner)
					release()
					s.strong.notifyActive(name, av.AttemptID, ev.Index, owner)
				}
			}
			// Dot is the op's raft index (ev.Index), authoritative regardless of
			// whether the snapshot Entry carries Epoch.
			s.translateActive(name, ev.Current.Value, ev.Index, false)
		} else {
			s.translateActive(name, nil, ev.Index, true)
			if s.strong != nil && ev.Previous != nil {
				av, err := decodeActive(ev.Previous.Value)
				if err != nil {
					return fmt.Errorf("registry record %q: %w", key, err)
				}
				if err := validateNamingRecord(key, activePrefix, av.Name, av.PID); err != nil {
					return err
				}
				if av.Strong {
					release := s.strong.admission.Acquire(name)
					s.strong.onTerminal(name, av.AttemptID, ev.Revision)
					release()
				}
			}
		}
		// Active success and deletion evidence above are handled synchronously;
		// there is no blocking action left for this event.
	case strings.HasPrefix(key, pendingPrefix):
		if s.strong != nil {
			name := strings.TrimPrefix(key, pendingPrefix)
			if ev.Current != nil {
				header, err := decodePending(ev.Current.Value)
				if err != nil {
					return fmt.Errorf("registry record %q: %w", key, err)
				}
				if err := validateNamingRecord(key, pendingPrefix, header.Name, header.PID); err != nil {
					return err
				}
				if owner := s.strong.owner.Load(); owner != nil {
					owner.markAttempt(name, header.AttemptID)
				} else {
					s.strong.mark(name)
				}
			} else if ev.Previous != nil {
				header, err := decodePending(ev.Previous.Value)
				if err != nil {
					return fmt.Errorf("registry record %q: %w", key, err)
				}
				if err := validateNamingRecord(key, pendingPrefix, header.Name, header.PID); err != nil {
					return err
				}
				if err := s.transitionPendingDelete(name, header.AttemptID, ev.Revision); err != nil {
					return err
				}
			}
		}
	case strings.HasPrefix(key, resultPrefix):
		// The result Put and Delete commit together. Only the Put carries
		// historical terminal evidence; the final snapshot has no result key.
		if s.strong != nil && ev.Type == kvapi.WatchPut && ev.Current != nil {
			result, err := decodeTerminalResult(ev.Current.Value)
			if err != nil {
				return fmt.Errorf("registry record %q: %w", key, err)
			}
			if key != resultKey(result.Name, result.AttemptID) {
				return fmt.Errorf("registry record %q: terminal result key mismatch", key)
			}
			s.strong.deliver(result.Name, result.AttemptID, strongCompletion{
				out:      globalapi.RegisterOutcome{Epoch: result.Epoch, State: globalapi.RegisterStateExpired},
				terminal: &result,
			})
		}
	case strings.HasPrefix(key, ackPrefix), strings.HasPrefix(key, rejectPrefix):
		if s.strong != nil {
			prefix := ackPrefix
			if strings.HasPrefix(key, rejectPrefix) {
				prefix = rejectPrefix
			}
			name, attemptID, ok, err := s.strongVoteName(key, prefix)
			if err != nil {
				return err
			}
			if ok {
				if owner := s.strong.owner.Load(); owner != nil {
					owner.markAttempt(name, attemptID)
				} else {
					s.strong.mark(name)
				}
			}
		}
	}
	return nil
}

func (s *Service) transitionPendingDelete(name, deletedAttempt string, deletedRevision uint64) error {
	release := s.strong.admission.Acquire(name)
	// Publication of the full promotion transaction precedes its per-key
	// events. Observe the current active binding before retiring the pending
	// exclusion, while admission for this name is serialized.
	entries, observed, err := s.localRead.ReadLocalSnapshot([]string{pendingKey(name), activeKey(name)})
	if err != nil {
		release()
		return fmt.Errorf("observe pending deletion %q: %w", name, err)
	}
	active, exists := entries[activeKey(name)]
	if !exists {
		s.strong.onTerminal(name, deletedAttempt, deletedRevision)
		release()
		return nil
	}
	av, err := decodeActive(active.Value)
	if err != nil {
		release()
		return fmt.Errorf("registry record %q: %w", activeKey(name), err)
	}
	if err := validateNamingRecord(active.Key, activePrefix, av.Name, av.PID); err != nil {
		release()
		return err
	}
	if !av.Strong {
		s.strong.onTerminal(name, deletedAttempt, deletedRevision)
		release()
		return nil
	}
	ap, err := pid.ParsePID(av.PID)
	if err != nil {
		release()
		return fmt.Errorf("registry record %q: %w", activeKey(name), err)
	}
	s.strong.recordActive(name, av.AttemptID, active.Epoch, observed, ap)
	release()
	s.strong.notifyActive(name, av.AttemptID, active.Epoch, ap)
	return nil
}

var voteComponentUnescaper = strings.NewReplacer("%3A", ":", "%25", "%")

// strongVoteName routes only votes for a current required participant.
// Storage and record failures remain errors so reconciliation closes admission.
func (s *Service) strongVoteName(key, prefix string) (string, string, bool, error) {
	rest, ok := strings.CutPrefix(key, prefix)
	if !ok {
		return "", "", false, nil
	}
	encodedName, rest, ok := strings.Cut(rest, ":")
	if !ok {
		return "", "", false, nil
	}
	attempt, encodedNode, ok := strings.Cut(rest, ":")
	if !ok || attempt == "" || encodedNode == "" {
		return "", "", false, nil
	}
	name := voteComponentUnescaper.Replace(encodedName)
	node := voteComponentUnescaper.Replace(encodedNode)
	if voteComponent(name) != encodedName || voteComponent(node) != encodedNode {
		return "", "", false, nil
	}
	pending, err := s.engine.Get(pendingKey(name))
	if errors.Is(err, kvapi.ErrKeyNotFound) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("read vote pending %q: %w", name, err)
	}
	hdr, err := decodePending(pending.Value)
	if err != nil {
		return "", "", false, fmt.Errorf("registry record %q: %w", pending.Key, err)
	}
	if err := validateNamingRecord(pending.Key, pendingPrefix, hdr.Name, hdr.PID); err != nil {
		return "", "", false, err
	}
	if hdr.AttemptID != attempt || !contains(hdr.RequiredNodes, node) {
		return "", "", false, nil
	}
	return name, attempt, true, nil
}

// translateActive feeds one active-binding change into the dissem plane: the
// leader broadcasts it to the gossip mesh; followers seed their local cache. The
// dot is the raft index (Entry.Epoch on a put, the delete's apply Index on a
// tombstone), monotonic per-name so the cache converges.
func (s *Service) translateActive(name string, value []byte, raftIndex uint64, deleted bool) {
	if s.dissem == nil {
		return
	}
	ev := global.BindingEvent{Name: name, RaftIndex: raftIndex, Deleted: deleted}
	if !deleted {
		av, err := decodeActive(value)
		if err != nil {
			return
		}
		p, perr := pid.ParsePID(av.PID)
		if perr != nil {
			return
		}
		ev.PID = p
	}
	if s.leaderFn() {
		s.dissem.LeaderBroadcast(ev)
	} else {
		s.dissem.LocalApply(ev)
	}
}

func (st *strongState) reconcileAllPending() error {
	var recordErr error
	if err := st.svc.engine.Scan(pendingPrefix, func(e kvapi.Entry) bool {
		header, err := decodePending(e.Value)
		if err != nil {
			recordErr = fmt.Errorf("registry record %q: %w", e.Key, err)
			return false
		}
		if recordErr = validateNamingRecord(e.Key, pendingPrefix, header.Name, header.PID); recordErr != nil {
			return false
		}
		if owner := st.owner.Load(); owner != nil {
			if recordErr = owner.markScannedAttempt(header.Name, header.AttemptID); recordErr != nil {
				return false
			}
		}
		return true
	}); err != nil {
		return err
	}
	return recordErr
}
