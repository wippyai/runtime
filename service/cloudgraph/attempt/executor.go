// SPDX-License-Identifier: MPL-2.0

// Package attempt executes one whole-resource operation: it spawns the
// provider actor, consumes the durable signal mailbox as its single writer,
// and drives the operation FSM to a terminal state
// (docs/design/cloud-graph.md §9.4, PoC subset).
package attempt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	cgapi "github.com/wippyai/runtime/api/service/cloudgraph"
	topapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/service/cloudgraph/binding"
	"github.com/wippyai/runtime/service/cloudgraph/ledger"
	"github.com/wippyai/runtime/service/cloudgraph/plan"
	"github.com/wippyai/runtime/service/cloudgraph/resource"
	"go.uber.org/zap"
)

// SignalTopic is the relay topic provider actors send signal envelopes on.
const SignalTopic = "cg.signal"

// Default executor tuning.
const (
	DefaultGateTimeout      = 2 * time.Minute
	DefaultOperationTimeout = 15 * time.Minute
	DefaultPollInterval     = 500 * time.Millisecond
	DefaultHeartbeatEvery   = 10 * time.Second
	DefaultExitGrace        = 3 * time.Second
)

// pidGenerator, relayNode, topology, and processSpawner are the narrow
// runtime seams the executor uses; the real process/relay/topology types
// satisfy them structurally.
type pidGenerator interface {
	Generate(host pid.HostID) pid.PID
}

type relayNode interface {
	Attach(p pid.PID, ch chan *relay.Package) (context.CancelFunc, error)
	Send(pkg *relay.Package) error
}

type topology interface {
	Register(p pid.PID) error
	Remove(p pid.PID)
}

type processSpawner interface {
	Start(ctx context.Context, start *process.Start) (pid.PID, error)
}

// Deps carries the executor's runtime dependencies.
type Deps struct {
	Log        *zap.Logger
	PIDGen     pidGenerator
	Node       relayNode
	Topo       topology
	Procs      processSpawner
	Transcoder payload.Transcoder
	Store      *ledger.Store
	Providers  *binding.Registry
	HostID     pid.HostID
}

// Executor runs execute_operation activities.
type Executor struct {
	deps             Deps
	gateTimeout      time.Duration
	operationTimeout time.Duration
	pollInterval     time.Duration
	heartbeatEvery   time.Duration
	exitGrace        time.Duration
}

// Request identifies one execution attempt. Incarnation is the Temporal
// activity attempt number; Heartbeat may be nil.
type Request struct {
	Heartbeat   func(details ...any)
	OpID        string
	Incarnation int64
}

// Option tunes an Executor.
type Option func(*Executor)

// WithGateTimeout bounds the dispatch gate wait.
func WithGateTimeout(d time.Duration) Option {
	return func(e *Executor) { e.gateTimeout = d }
}

// WithOperationTimeout bounds one operation execution.
func WithOperationTimeout(d time.Duration) Option {
	return func(e *Executor) { e.operationTimeout = d }
}

// WithPollInterval sets the gate re-check interval.
func WithPollInterval(d time.Duration) Option {
	return func(e *Executor) { e.pollInterval = d }
}

// WithExitGrace sets how long the executor keeps draining trailing signals
// after the actor exit event before declaring the exit non-terminal.
func WithExitGrace(d time.Duration) Option {
	return func(e *Executor) { e.exitGrace = d }
}

// NewExecutor creates an executor with default tuning.
func NewExecutor(deps Deps, opts ...Option) *Executor {
	if deps.Log == nil {
		deps.Log = zap.NewNop()
	}
	e := &Executor{
		deps:             deps,
		gateTimeout:      DefaultGateTimeout,
		operationTimeout: DefaultOperationTimeout,
		pollInterval:     DefaultPollInterval,
		heartbeatEvery:   DefaultHeartbeatEvery,
		exitGrace:        DefaultExitGrace,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// signalWire is the envelope provider actors send on SignalTopic.
type signalWire struct {
	Kind    string          `json:"kind"`
	Phase   string          `json:"phase,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Seq     int64           `json:"seq"`
}

// terminalPayload is the payload shape of verify_passed and failed signals.
type terminalPayload struct {
	Error   string          `json:"error,omitempty"`
	Outputs json.RawMessage `json:"outputs,omitempty"`
}

// Execute runs one operation to a terminal state and returns its persisted
// result JSON (design §9.4).
func (e *Executor) Execute(ctx context.Context, req Request) (json.RawMessage, error) {
	store := e.deps.Store
	log := e.deps.Log.With(zap.String("operation", req.OpID), zap.Int64("incarnation", req.Incarnation))

	op, err := store.GetOperation(ctx, req.OpID)
	if err != nil {
		return nil, err
	}
	if op.Status.Terminal() {
		if op.Status == resource.OpCommitted {
			return op.Result, nil
		}
		return nil, NewOperationFailedError(op.ID, op.Error)
	}

	priorActorPID := op.ActorPID

	ok, err := store.BumpIncarnation(ctx, op.ID, req.Incarnation)
	if err != nil {
		return nil, err
	}
	if !ok {
		current, err := store.GetOperation(ctx, req.OpID)
		if err != nil {
			return nil, err
		}
		if current.Status == resource.OpCommitted {
			return current.Result, nil
		}
		if current.Status == resource.OpFailed {
			return nil, NewOperationFailedError(current.ID, current.Error)
		}
		return nil, NewStaleIncarnationError(op.ID)
	}
	op.Incarnation = req.Incarnation

	deploy, err := store.GetDeploy(ctx, op.DeployID)
	if err != nil {
		return nil, err
	}
	spec, err := store.GetSpec(ctx, op.DeployID, op.ResourceID)
	if err != nil {
		return nil, err
	}
	manifest, err := e.deps.Providers.Provider(spec.ProviderType)
	if err != nil {
		return nil, err
	}

	collectorPID := e.deps.PIDGen.Generate(topapi.ControlHost)
	if err := e.deps.Topo.Register(collectorPID); err != nil {
		return nil, err
	}
	defer e.deps.Topo.Remove(collectorPID)

	monitorCh := make(chan *relay.Package, 16)
	detach, err := e.deps.Node.Attach(collectorPID, monitorCh)
	if err != nil {
		return nil, err
	}
	defer detach()

	e.reapOrphan(collectorPID, priorActorPID, log)

	if err := e.waitForGates(ctx, op, req.Heartbeat, log); err != nil {
		e.terminalizeIfPermanent(ctx, op, err, log)
		return nil, err
	}

	desired, err := e.materialize(ctx, spec.Desired)
	if err != nil {
		e.terminalizeIfPermanent(ctx, op, err, log)
		return nil, err
	}

	checkpoint, err := store.GetLatestCheckpoint(ctx, op.ID)
	if err != nil {
		return nil, err
	}

	actorPID, err := e.spawnActor(ctx, collectorPID, manifest, op, spec, desired, checkpoint)
	if err != nil {
		return nil, err
	}
	if err := store.SetActorPID(ctx, op.ID, op.Incarnation, actorPID.String()); err != nil {
		e.cancelActor(collectorPID, actorPID, "actor pid persistence failed", log)
		return nil, err
	}
	log.Info("provider actor spawned", zap.String("pid", actorPID.String()))

	return e.consumeSignals(ctx, op, spec, deploy.Scope, collectorPID, actorPID, monitorCh, req.Heartbeat, log)
}

// terminalizeIfPermanent writes the failed terminal status only for
// permanent domain failures (gate timeout, failed dependency, missing
// output). Context cancellation and transient store errors pass through
// untouched so the Temporal retry policy stays in charge.
func (e *Executor) terminalizeIfPermanent(ctx context.Context, op *resource.Operation, cause error, log *zap.Logger) {
	var ae apierror.Error
	if !errors.As(cause, &ae) || ae.Retryable() != apierror.False {
		return
	}
	if _, err := e.deps.Store.Terminalize(ctx, op.ID, op.Incarnation,
		[]resource.OperationStatus{resource.OpDispatched, resource.OpInProgress},
		resource.OpFailed, cause.Error()); err != nil {
		log.Error("terminalize after permanent failure", zap.Error(err))
	}
}

// reapOrphan cancels a live actor left by a previous incarnation
// (design §9.4 step 3). Signal fencing already excludes it; this only frees
// the process.
func (e *Executor) reapOrphan(collectorPID pid.PID, priorActorPID string, log *zap.Logger) {
	if priorActorPID == "" {
		return
	}
	orphan, err := pid.ParsePID(priorActorPID)
	if err != nil {
		log.Warn("unparseable prior actor pid", zap.String("pid", priorActorPID), zap.Error(err))
		return
	}
	if err := e.deps.Node.Send(topapi.CancelPackage(collectorPID, orphan, "superseded actor incarnation")); err != nil {
		log.Debug("orphan cancel not delivered", zap.String("pid", priorActorPID), zap.Error(err))
	}
}

// waitForGates re-checks every inbound hard dependency against live ledger
// state until satisfied or timed out (design §9.4 step 5).
func (e *Executor) waitForGates(ctx context.Context, op *resource.Operation, heartbeat func(...any), log *zap.Logger) error {
	edges, err := e.deps.Store.ListEdgesFrom(ctx, op.DeployID, op.ResourceID)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(e.gateTimeout)
	lastBeat := time.Now()
	for {
		unsatisfied, err := e.unsatisfiedGate(ctx, op, edges)
		if err != nil {
			return err
		}
		if unsatisfied == "" {
			return nil
		}
		if time.Now().After(deadline) {
			return NewGateTimeoutError(op.ID, unsatisfied)
		}
		if heartbeat != nil && time.Since(lastBeat) >= e.heartbeatEvery {
			heartbeat("gate-wait")
			lastBeat = time.Now()
		}
		log.Debug("gate unsatisfied, waiting", zap.String("gate", unsatisfied))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(e.pollInterval):
		}
	}
}

// unsatisfiedGate evaluates the gate predicates of §5.4 (PoC subset) and
// returns a description of the first unsatisfied gate, or "".
func (e *Executor) unsatisfiedGate(ctx context.Context, op *resource.Operation, edges []resource.Edge) (string, error) {
	for _, edge := range edges {
		if edge.Kind == resource.DepSoft {
			continue
		}

		target, err := e.deps.Store.GetOperationByResource(ctx, op.DeployID, edge.TargetResourceID)
		if err != nil {
			return "", err
		}
		if target.Status == resource.OpFailed {
			return "", NewOperationFailedError(op.ID, "dependency "+edge.TargetResourceID+" failed")
		}
		if target.Status != resource.OpCommitted {
			return fmt.Sprintf("%s (%s) not committed", edge.TargetResourceID, edge.Kind), nil
		}

		if edge.Kind == resource.DepConfigure || edge.Kind == resource.DepAttachment {
			inst, err := e.deps.Store.GetInstance(ctx, edge.TargetResourceID)
			if err != nil {
				return "", err
			}
			if len(inst.Outputs) == 0 {
				return fmt.Sprintf("%s (%s) has no outputs", edge.TargetResourceID, edge.Kind), nil
			}
		}
	}
	return "", nil
}

// materialize rewrites every $ref in the desired document with the committed
// producer outputs (design §9.4 step 6).
func (e *Executor) materialize(ctx context.Context, desired json.RawMessage) (json.RawMessage, error) {
	return plan.Materialize(desired, func(ref plan.OutputRef) (any, error) {
		inst, err := e.deps.Store.GetInstance(ctx, ref.ResourceID)
		if err != nil {
			return nil, err
		}
		var outputs map[string]any
		if len(inst.Outputs) > 0 {
			if err := json.Unmarshal(inst.Outputs, &outputs); err != nil {
				return nil, err
			}
		}
		value, ok := outputs[ref.OutputKey]
		if !ok {
			return nil, NewMissingOutputError(ref.ResourceID, ref.OutputKey)
		}
		return value, nil
	})
}

// spawnActor starts the provider actor with the collector PID and the
// assembled operation input (design §9.4 steps 6-7).
func (e *Executor) spawnActor(ctx context.Context, collectorPID pid.PID, manifest *cgapi.ProviderConfig, op *resource.Operation, spec *resource.Spec, desired, checkpoint json.RawMessage) (pid.PID, error) {
	var desiredDoc any
	if len(desired) > 0 {
		if err := json.Unmarshal(desired, &desiredDoc); err != nil {
			return pid.PID{}, err
		}
	}
	var checkpointDoc any
	if len(checkpoint) > 0 {
		if err := json.Unmarshal(checkpoint, &checkpointDoc); err != nil {
			return pid.PID{}, err
		}
	}

	input := map[string]any{
		"operation_id":  op.ID,
		"incarnation":   op.Incarnation,
		"resource_id":   op.ResourceID,
		"resource_type": spec.Type,
		"desired":       desiredDoc,
		"checkpoint":    checkpointDoc,
		"executor":      manifest.Executor.String(),
		"collector":     collectorPID.String(),
	}

	options := attrs.NewBag()
	options.Set(process.ProcessParentKey, collectorPID)
	options.Set(process.ProcessMonitorKey, true)

	return e.deps.Procs.Start(ctx, &process.Start{
		HostID:  e.deps.HostID,
		Source:  manifest.ActorProcess,
		Input:   payload.Payloads{payload.New(input)},
		Options: options,
	})
}

// consumeSignals is the single-writer select loop: it appends provider
// signals to the mailbox under the CAS fence and steps the FSM on acceptance
// (design §9.4 step 8).
func (e *Executor) consumeSignals(
	ctx context.Context,
	op *resource.Operation,
	spec *resource.Spec,
	scope string,
	collectorPID, actorPID pid.PID,
	monitorCh chan *relay.Package,
	heartbeat func(...any),
	log *zap.Logger,
) (json.RawMessage, error) {
	store := e.deps.Store
	fsm := &signalFSM{}
	firstAccepted := false

	heartbeatTicker := time.NewTicker(e.heartbeatEvery)
	defer heartbeatTicker.Stop()
	opDeadline := time.NewTimer(e.operationTimeout)
	defer opDeadline.Stop()

	// A nil channel blocks forever; it becomes live once the actor exit event
	// arrives, giving trailing signals a bounded drain window.
	var exitGrace <-chan time.Time

	fail := func(detail string) (json.RawMessage, error) {
		if _, err := store.Terminalize(ctx, op.ID, op.Incarnation,
			[]resource.OperationStatus{resource.OpDispatched, resource.OpInProgress},
			resource.OpFailed, detail); err != nil {
			log.Error("terminalize failed", zap.Error(err))
		}
		return nil, NewOperationFailedError(op.ID, detail)
	}

	for {
		select {
		case <-ctx.Done():
			e.cancelActor(collectorPID, actorPID, "activity context canceled", log)
			return nil, ctx.Err()

		case <-heartbeatTicker.C:
			if heartbeat != nil {
				heartbeat(op.LastSignal)
			}

		case <-opDeadline.C:
			e.cancelActor(collectorPID, actorPID, "operation timeout", log)
			if _, err := store.Terminalize(ctx, op.ID, op.Incarnation,
				[]resource.OperationStatus{resource.OpDispatched, resource.OpInProgress},
				resource.OpFailed, "operation timeout"); err != nil {
				log.Error("terminalize after timeout", zap.Error(err))
			}
			return nil, NewOperationTimeoutError(op.ID)

		case <-exitGrace:
			log.Warn("actor exited without terminal signal")
			return nil, NewActorExitError(op.ID)

		case batch, chOpen := <-monitorCh:
			if !chOpen {
				return nil, NewActorExitError(op.ID)
			}
			for _, msg := range batch.Messages {
				switch msg.Topic {
				case topapi.TopicEvents:
					for _, p := range msg.Payloads {
						if _, isExit := p.Data().(*topapi.ExitEvent); isExit && exitGrace == nil {
							exitGrace = time.After(e.exitGrace)
						}
					}

				case SignalTopic:
					for _, p := range msg.Payloads {
						sig, err := e.decodeSignal(op, p)
						if err != nil {
							log.Warn("undecodable signal", zap.Error(err))
							continue
						}

						accepted, err := store.AppendSignal(ctx, *sig)
						if err != nil {
							return nil, err
						}
						if !accepted {
							log.Warn("signal rejected by mailbox fence",
								zap.Int64("seq", sig.Seq), zap.String("phase", string(sig.Phase)))
							continue
						}
						op.LastSignal = sig.Seq

						if !firstAccepted {
							firstAccepted = true
							if err := store.MarkInProgress(ctx, op.ID, op.Incarnation); err != nil {
								return nil, err
							}
						}

						outcome, err := fsm.step(*sig)
						if err != nil {
							e.cancelActor(collectorPID, actorPID, "illegal signal", log)
							return fail(err.Error())
						}

						switch outcome {
						case stepCommit:
							var term terminalPayload
							if len(sig.Payload) > 0 {
								if err := json.Unmarshal(sig.Payload, &term); err != nil {
									return fail("undecodable verify_passed payload: " + err.Error())
								}
							}
							result, err := json.Marshal(map[string]any{
								"resource_id": op.ResourceID,
								"outputs":     orEmpty(term.Outputs),
							})
							if err != nil {
								return nil, err
							}
							if err := store.CommitOperation(ctx, op, spec, scope, term.Outputs, result); err != nil {
								return nil, err
							}
							log.Info("operation committed")
							return result, nil

						case stepFailed:
							var term terminalPayload
							if len(sig.Payload) > 0 {
								if err := json.Unmarshal(sig.Payload, &term); err != nil {
									term.Error = string(sig.Payload)
								}
							}
							if term.Error == "" {
								term.Error = "provider reported failure"
							}
							return fail(term.Error)

						case stepContinue:
						}
					}
				}
			}
		}
	}
}

// decodeSignal converts one relay payload into a fenced signal envelope.
func (e *Executor) decodeSignal(op *resource.Operation, p payload.Payload) (*resource.Signal, error) {
	var wire signalWire
	if err := e.deps.Transcoder.Unmarshal(p, &wire); err != nil {
		return nil, err
	}
	return &resource.Signal{
		OperationID: op.ID,
		Incarnation: op.Incarnation,
		Seq:         wire.Seq,
		Kind:        resource.SignalKind(wire.Kind),
		Phase:       resource.SignalPhaseName(wire.Phase),
		Payload:     wire.Payload,
	}, nil
}

// cancelActor delivers the cancel package to the actor (PoC: single phase of
// the three-phase protocol; the rest is documented as skipped).
func (e *Executor) cancelActor(collectorPID, actorPID pid.PID, reason string, log *zap.Logger) {
	if err := e.deps.Node.Send(topapi.CancelPackage(collectorPID, actorPID, reason)); err != nil {
		log.Warn("cancel not delivered", zap.Error(err))
	}
}

func orEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}
