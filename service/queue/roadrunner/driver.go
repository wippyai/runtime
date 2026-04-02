// SPDX-License-Identifier: MPL-2.0

package roadrunner

import (
	"context"
	"fmt"
	"net"
	"net/rpc"
	"net/url"
	"sync"

	"github.com/google/uuid"
	jobsProto "github.com/roadrunner-server/api/v4/build/jobs/v1"
	goridgeRpc "github.com/roadrunner-server/goridge/v3/pkg/rpc"
	"github.com/wippyai/runtime/api/attrs"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	queuesvc "github.com/wippyai/runtime/service/queue"
	"go.uber.org/zap"
)

// Driver implements the RoadRunner queue driver.
// It connects to a running RoadRunner instance via goridge RPC for publishing
// jobs, declaring pipelines, and querying stats. Consumption (Attach) is not
// supported because RoadRunner manages its own worker pool for job processing.
type Driver struct {
	ctx        context.Context
	logger     *zap.Logger
	client     *rpc.Client
	rpcAddr    string
	pipeline   string
	queues     map[registry.ID]*declaredQueue
	cancel     context.CancelFunc
	statusChan chan any
	id         registry.ID
	mu         sync.RWMutex
}

type declaredQueue struct {
	opts     attrs.Attributes
	pipeline string
}

// NewDriver creates a new RoadRunner driver instance.
func NewDriver(id registry.ID, rpcAddr, pipeline string, logger *zap.Logger) *Driver {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Driver{
		id:       id,
		rpcAddr:  rpcAddr,
		pipeline: pipeline,
		logger:   logger,
		queues:   make(map[registry.ID]*declaredQueue),
	}
}

func (d *Driver) pipelineName(queueID registry.ID, opts attrs.Attributes) string {
	if opts != nil {
		if name := opts.GetString(queueapi.OptionQueueName, ""); name != "" {
			return name
		}
	}
	if d.pipeline != "" {
		return d.pipeline
	}
	return queueID.Name
}

func (d *Driver) Publish(_ context.Context, queueID registry.ID, msgs ...*queueapi.Message) error {
	d.mu.RLock()
	q, exists := d.queues[queueID]
	client := d.client
	d.mu.RUnlock()

	if !exists {
		return queueapi.ErrQueueNotFound
	}
	if client == nil {
		return queuesvc.ErrDriverNotStarted
	}

	for _, msg := range msgs {
		if msg.ID == "" {
			msg.ID = uuid.New().String()
		}

		headers := make(map[string]*jobsProto.HeaderValue)
		if msg.Headers != nil {
			for k, v := range msg.Headers {
				headers[k] = &jobsProto.HeaderValue{
					Value: []string{fmt.Sprintf("%v", v)},
				}
			}
		}

		body, err := marshalBody(msg.Body)
		if err != nil {
			return fmt.Errorf("roadrunner marshal body: %w", err)
		}

		req := &jobsProto.PushRequest{
			Job: &jobsProto.Job{
				Job:     queueID.Name,
				Id:      msg.ID,
				Payload: body,
				Headers: headers,
				Options: &jobsProto.Options{
					Pipeline: q.pipeline,
					Priority: int64(msg.Headers.GetInt(queueapi.HeaderPriority, 0)),
				},
			},
		}

		resp := &jobsProto.Empty{}
		if err := client.Call("jobs.Push", req, resp); err != nil {
			return fmt.Errorf("roadrunner push: %w", err)
		}
	}

	return nil
}

// Attach is not supported for the RoadRunner driver.
// Job consumption is managed by RoadRunner's worker pool. Configure workers in
// your .rr.yaml to process jobs from RoadRunner pipelines.
func (d *Driver) Attach(_ context.Context, _ registry.ID, _ chan<- *queueapi.Delivery) (context.CancelFunc, error) {
	return nil, fmt.Errorf("roadrunner driver does not support Attach; consumption is handled by RoadRunner's worker pool")
}

func (d *Driver) DeclareQueue(_ context.Context, queueID registry.ID, opts attrs.Attributes) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.queues[queueID]; exists {
		return nil
	}

	pipeline := d.pipelineName(queueID, opts)

	d.queues[queueID] = &declaredQueue{
		pipeline: pipeline,
		opts:     opts,
	}

	d.logger.Debug("queue declared",
		zap.String("driver", d.id.String()),
		zap.String("queue", queueID.String()),
		zap.String("pipeline", pipeline))

	return nil
}

func (d *Driver) GetQueueInfo(_ context.Context, queueID registry.ID) (attrs.Attributes, error) {
	d.mu.RLock()
	q, exists := d.queues[queueID]
	client := d.client
	d.mu.RUnlock()

	if !exists {
		return nil, queueapi.ErrQueueNotFound
	}
	if client == nil {
		return nil, queuesvc.ErrDriverNotStarted
	}

	resp := &jobsProto.Stats{}
	if err := client.Call("jobs.Stat", &jobsProto.Empty{}, resp); err != nil {
		return nil, fmt.Errorf("roadrunner stat: %w", err)
	}

	for _, stat := range resp.GetStats() {
		if stat.GetPipeline() == q.pipeline {
			info := attrs.NewBag()
			info.Set(queueapi.StatsMessageCount, int(stat.GetActive()+stat.GetDelayed()+stat.GetReserved()))
			info.Set(queueapi.StatsReady, int(stat.GetActive()))
			return info, nil
		}
	}

	return attrs.NewBag(), nil
}

func (d *Driver) Start(ctx context.Context) (<-chan any, error) {
	network, addr, err := parseRPCAddr(d.rpcAddr)
	if err != nil {
		return nil, fmt.Errorf("roadrunner parse rpc addr: %w", err)
	}

	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("roadrunner dial: %w", err)
	}

	client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))

	d.mu.Lock()
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.client = client
	d.statusChan = make(chan any, 1)
	d.mu.Unlock()

	d.logger.Info("roadrunner driver started",
		zap.String("id", d.id.String()),
		zap.String("rpc_addr", d.rpcAddr))

	return d.statusChan, nil
}

func (d *Driver) Stop(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cancel != nil {
		d.cancel()
	}

	if d.client != nil {
		d.client.Close()
		d.client = nil
	}

	d.queues = make(map[registry.ID]*declaredQueue)

	if d.statusChan != nil {
		close(d.statusChan)
	}

	d.logger.Info("roadrunner driver stopped", zap.String("id", d.id.String()))
	return nil
}

// parseRPCAddr parses a RoadRunner RPC address like "tcp://127.0.0.1:6001"
// into network and address components.
func parseRPCAddr(addr string) (network, address string, err error) {
	u, err := url.Parse(addr)
	if err != nil {
		return "", "", err
	}

	switch u.Scheme {
	case "tcp", "tcp4", "tcp6":
		return u.Scheme, u.Host, nil
	case "unix":
		return "unix", u.Path, nil
	default:
		return "", "", fmt.Errorf("unsupported rpc scheme: %s", u.Scheme)
	}
}
