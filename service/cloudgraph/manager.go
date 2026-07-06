// SPDX-License-Identifier: MPL-2.0

package cloudgraph

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/resource"
	apiruntime "github.com/wippyai/runtime/api/runtime"
	cgapi "github.com/wippyai/runtime/api/service/cloudgraph"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	topapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/service/cloudgraph/attempt"
	"github.com/wippyai/runtime/service/cloudgraph/binding"
	"github.com/wippyai/runtime/service/cloudgraph/ledger"
	"github.com/wippyai/runtime/service/cloudgraph/plan"
	cgresource "github.com/wippyai/runtime/service/cloudgraph/resource"
	cgworkflow "github.com/wippyai/runtime/service/cloudgraph/workflow"
	servicesql "github.com/wippyai/runtime/service/sql"
	"github.com/wippyai/runtime/system/entry"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	sdkclient "go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

// Function-registry IDs of the engine's Go handlers. One engine per
// installation (design §1: one engine instance owns one shard).
const (
	FuncSubmitIntent     = "wippy.cloudgraph:submit_intent"
	FuncGetDeploy        = "wippy.cloudgraph:get_deploy"
	FuncComputePlan      = "wippy.cloudgraph:cg_compute_plan"
	FuncExecuteOperation = "wippy.cloudgraph:cg_execute_operation"
	FuncFinalizeDeploy   = "wippy.cloudgraph:cg_finalize_deploy"
)

// Manager handles cloudgraph.engine and cloudgraph.provider entries.
type Manager struct {
	log       *zap.Logger
	bus       event.Bus
	dtt       payload.Transcoder
	pidGen    process.PIDGenerator
	node      relay.Node
	topo      topapi.Topology
	procs     process.Manager
	registrar temporalapi.WorkerRegistrar
	providers *binding.Registry
	engine    *engineRuntime
	mu        sync.Mutex
}

// engineRuntime is the state built when the cloudgraph.engine entry loads.
type engineRuntime struct {
	cfg       *cgapi.EngineConfig
	store     *ledger.Store
	releaseDB func()
}

// NewManager creates the cloud-graph manager.
func NewManager(
	log *zap.Logger,
	bus event.Bus,
	dtt payload.Transcoder,
	pidGen process.PIDGenerator,
	node relay.Node,
	topo topapi.Topology,
	procs process.Manager,
	registrar temporalapi.WorkerRegistrar,
) *Manager {
	if log == nil {
		log = zap.NewNop()
	}
	return &Manager{
		log:       log,
		bus:       bus,
		dtt:       dtt,
		pidGen:    pidGen,
		node:      node,
		topo:      topo,
		procs:     procs,
		registrar: registrar,
		providers: binding.NewRegistry(),
	}
}

// Add implements registry.EntryListener.
func (m *Manager) Add(ctx context.Context, ent registry.Entry) error {
	switch ent.Kind {
	case cgapi.Provider:
		return m.addProvider(ctx, ent)
	case cgapi.Engine:
		return m.addEngine(ctx, ent)
	default:
		return nil
	}
}

// Update implements registry.EntryListener. Manifests and the engine are
// immutable for the process lifetime (design I20).
func (m *Manager) Update(_ context.Context, ent registry.Entry) error {
	m.log.Warn("cloudgraph entries are immutable at runtime; update ignored",
		zap.String("id", ent.ID.String()))
	return nil
}

// Delete implements registry.EntryListener.
func (m *Manager) Delete(_ context.Context, ent registry.Entry) error {
	if ent.Kind != cgapi.Engine {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.engine != nil && m.engine.releaseDB != nil {
		m.engine.releaseDB()
	}
	m.engine = nil
	return nil
}

func (m *Manager) addProvider(ctx context.Context, ent registry.Entry) error {
	cfg, err := entry.DecodeEntryConfig[cgapi.ProviderConfig](ctx, m.dtt, ent)
	if err != nil {
		return err
	}
	if err := m.providers.Register(cfg); err != nil {
		return err
	}
	m.log.Info("provider manifest registered",
		zap.String("provider_type", cfg.ProviderType),
		zap.String("actor", cfg.ActorProcess.String()))
	return nil
}

func (m *Manager) addEngine(ctx context.Context, ent registry.Entry) error {
	cfg, err := entry.DecodeEntryConfig[cgapi.EngineConfig](ctx, m.dtt, ent)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.engine != nil {
		return NewEngineConflictError(ent.ID.String())
	}

	reg := resource.GetRegistry(ctx)
	if reg == nil {
		return ErrResourceRegistryMissing
	}
	res, err := reg.Acquire(ctx, cfg.Database, resource.ModeNormal)
	if err != nil {
		return err
	}
	conn, err := res.Get()
	if err != nil {
		res.Release()
		return err
	}
	dbRes, ok := conn.(servicesql.DBResource)
	if !ok {
		res.Release()
		return ErrInvalidDatabaseResource
	}

	store := ledger.NewStore(dbRes.DB, m.log.Named("ledger"))
	if err := store.Migrate(ctx); err != nil {
		res.Release()
		return err
	}

	executor := attempt.NewExecutor(attempt.Deps{
		Log:        m.log.Named("executor"),
		PIDGen:     m.pidGen,
		Node:       m.node,
		Topo:       m.topo,
		Procs:      m.procs,
		Transcoder: m.dtt,
		Store:      store,
		Providers:  m.providers,
		HostID:     cfg.Host.String(),
	})

	activities := &cgworkflow.Activities{
		Store:      store,
		Executor:   executor,
		Transcoder: m.dtt,
		Log:        m.log.Named("activities"),
	}

	m.publishFunc(ctx, FuncSubmitIntent, m.submitIntent)
	m.publishFunc(ctx, FuncGetDeploy, m.getDeploy)
	m.publishFunc(ctx, FuncComputePlan, activities.ComputePlan)
	m.publishFunc(ctx, FuncExecuteOperation, activities.ExecuteOperation)
	m.publishFunc(ctx, FuncFinalizeDeploy, activities.FinalizeDeploy)

	pairs := []struct {
		name   string
		funcID string
	}{
		{cgworkflow.ActivityComputePlan, FuncComputePlan},
		{cgworkflow.ActivityExecuteOperation, FuncExecuteOperation},
		{cgworkflow.ActivityFinalizeDeploy, FuncFinalizeDeploy},
	}
	for _, pair := range pairs {
		if err := m.registrar.RegisterActivity(ctx, cfg.Worker, pair.name, registry.ParseID(pair.funcID)); err != nil {
			res.Release()
			return err
		}
	}
	if err := m.registrar.RegisterWorkflow(ctx, cfg.Worker, cfg.WorkflowName, cgworkflow.Deploy); err != nil {
		res.Release()
		return err
	}

	m.engine = &engineRuntime{cfg: cfg, store: store, releaseDB: res.Release}
	m.log.Info("cloudgraph engine ready",
		zap.String("id", ent.ID.String()),
		zap.String("shard", cfg.Shard),
		zap.String("worker", cfg.Worker.String()))
	return nil
}

func (m *Manager) publishFunc(ctx context.Context, id string, handler function.Func) {
	m.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.FunctionRegister,
		Path:   id,
		Data:   &function.FuncEntry{Handler: handler},
	})
}

func (m *Manager) runtime() (*engineRuntime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.engine == nil {
		return nil, ErrEngineNotReady
	}
	return m.engine, nil
}

// submitIntent is the deploy intake: idempotent persist, then start the
// Temporal deploy workflow with the deploy-scoped workflow ID (design §6.1).
func (m *Manager) submitIntent(ctx context.Context, task apiruntime.Task) (*apiruntime.Result, error) {
	eng, err := m.runtime()
	if err != nil {
		return nil, err
	}
	if len(task.Payloads) == 0 {
		return nil, NewInvalidDeployInputError("missing deploy input")
	}

	var in cgresource.DeployInput
	if err := m.dtt.Unmarshal(task.Payloads[0], &in); err != nil {
		return nil, NewInvalidDeployInputError(err.Error())
	}
	if in.DeployID == "" || in.Scope == "" || len(in.Specs) == 0 {
		return nil, NewInvalidDeployInputError("deploy_id, scope, and specs are required")
	}
	for _, spec := range in.Specs {
		if _, err := m.providers.Provider(spec.ProviderType); err != nil {
			return nil, err
		}
	}

	created, err := eng.store.CreateDeploy(ctx, in)
	if err != nil {
		return nil, err
	}
	if !created {
		existing, err := eng.store.LoadSpecs(ctx, in.DeployID)
		if err != nil {
			return nil, err
		}
		existingHash, err := plan.SpecHash(existing)
		if err != nil {
			return nil, err
		}
		submittedHash, err := plan.SpecHash(in.Specs)
		if err != nil {
			return nil, err
		}
		if existingHash != submittedHash {
			return nil, NewInvalidDeployInputError("deploy_id " + in.DeployID + " already exists with different specs")
		}
	}

	if err := m.startDeployWorkflow(ctx, eng, in.DeployID); err != nil {
		return nil, err
	}

	rec, err := eng.store.GetDeploy(ctx, in.DeployID)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	return &apiruntime.Result{Value: payload.New(string(out))}, nil
}

func (m *Manager) startDeployWorkflow(ctx context.Context, eng *engineRuntime, deployID string) error {
	reg := resource.GetRegistry(ctx)
	if reg == nil {
		return ErrResourceRegistryMissing
	}
	res, err := reg.Acquire(ctx, eng.cfg.Client, resource.ModeNormal)
	if err != nil {
		return err
	}
	defer res.Release()

	conn, err := res.Get()
	if err != nil {
		return err
	}
	clientRes, ok := conn.(temporalapi.ClientResource)
	if !ok {
		return ErrInvalidClientResource
	}

	workerCfg, ok := m.registrar.GetConfig(eng.cfg.Worker)
	if !ok {
		return NewWorkerNotFoundError(eng.cfg.Worker.String())
	}

	_, err = clientRes.Client.ExecuteWorkflow(ctx, sdkclient.StartWorkflowOptions{
		ID:                    "cloudgraph/deploy/" + deployID,
		TaskQueue:             clientRes.GetTaskQueueName(workerCfg.TaskQueue),
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
	}, eng.cfg.WorkflowName, deployID)

	var already *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(err, &already) {
		return nil
	}
	return err
}

// getDeploy returns the deploy record with its operations for polling.
func (m *Manager) getDeploy(ctx context.Context, task apiruntime.Task) (*apiruntime.Result, error) {
	eng, err := m.runtime()
	if err != nil {
		return nil, err
	}
	if len(task.Payloads) == 0 {
		return nil, NewInvalidDeployInputError("missing deploy_id")
	}

	var deployID string
	if err := m.dtt.Unmarshal(task.Payloads[0], &deployID); err != nil {
		return nil, NewInvalidDeployInputError(err.Error())
	}

	rec, err := eng.store.GetDeploy(ctx, deployID)
	if err != nil {
		return nil, err
	}
	ops, err := eng.store.ListOperations(ctx, deployID)
	if err != nil {
		return nil, err
	}

	out, err := json.Marshal(struct {
		Deploy     *cgresource.DeployRecord `json:"deploy"`
		Operations []cgresource.Operation   `json:"operations"`
	}{Deploy: rec, Operations: ops})
	if err != nil {
		return nil, err
	}
	return &apiruntime.Result{Value: payload.New(string(out))}, nil
}

var _ registry.EntryListener = (*Manager)(nil)
