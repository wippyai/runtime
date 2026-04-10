// SPDX-License-Identifier: MPL-2.0

package redis

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	redisapi "github.com/wippyai/runtime/api/service/queue/redis"
	queuesvc "github.com/wippyai/runtime/service/queue"
	"go.uber.org/zap"
)

const (
	// consumerGroup is the default consumer group name used by the driver.
	consumerGroup = "wippy"
	// blockTimeout is the duration to block on XREADGROUP.
	blockTimeout = 5 * time.Second
)

type declaredQueue struct {
	opts   attrs.Attributes
	stream string
}

// Driver implements the Redis Streams queue driver.
type Driver struct {
	ctx           context.Context
	logger        *zap.Logger
	client        goredis.UniversalClient
	tc            payload.Transcoder
	opts          *goredis.UniversalOptions
	queues        map[registry.ID]*declaredQueue
	cancel        context.CancelFunc
	statusChan    chan any
	id            registry.ID
	claimInterval time.Duration // how often to run XAUTOCLAIM; 0 disables
	claimMinIdle  time.Duration // min idle time for a pending message to be claimable
	mu            sync.RWMutex
}

// NewDriver creates a new Redis Streams driver instance.
func NewDriver(id registry.ID, opts *goredis.UniversalOptions, tc payload.Transcoder, logger *zap.Logger) *Driver {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Driver{
		id:            id,
		opts:          opts,
		tc:            tc,
		logger:        logger,
		queues:        make(map[registry.ID]*declaredQueue),
		claimInterval: redisapi.DefaultClaimInterval,
		claimMinIdle:  redisapi.DefaultClaimMinIdle,
	}
}

// NewDriverWithConfig creates a new Redis Streams driver instance with
// claim recovery settings from the full config.
func NewDriverWithConfig(id registry.ID, opts *goredis.UniversalOptions, cfg *redisapi.Config, tc payload.Transcoder, logger *zap.Logger) *Driver {
	d := NewDriver(id, opts, tc, logger)
	if cfg.ClaimInterval != 0 {
		d.claimInterval = cfg.ClaimInterval
	}
	if cfg.ClaimMinIdle != 0 {
		d.claimMinIdle = cfg.ClaimMinIdle
	}
	return d
}

func (d *Driver) streamName(queueID registry.ID, opts attrs.Attributes) string {
	if opts != nil {
		if name := opts.GetString(queueapi.OptionQueueName, ""); name != "" {
			return name
		}
	}
	return queueID.Name
}

func (d *Driver) Publish(ctx context.Context, queueID registry.ID, msgs ...*queueapi.Message) error {
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

		body, err := marshalBody(d.tc, queueCodec(q.opts), msg.Body)
		if err != nil {
			return fmt.Errorf("redis marshal body: %w", err)
		}

		values := map[string]any{
			"id":   msg.ID,
			"body": string(body),
		}

		if msg.Headers != nil {
			headers, err := marshalHeaders(msg.Headers)
			if err != nil {
				return fmt.Errorf("redis marshal headers: %w", err)
			}
			values["headers"] = string(headers)
		}

		maxLen := int64(0)
		if q.opts != nil {
			maxLen = int64(q.opts.GetInt(queueapi.OptionMaxLength, 0))
		}

		args := &goredis.XAddArgs{
			Stream: q.stream,
			Values: values,
		}
		if maxLen > 0 {
			args.MaxLen = maxLen
			args.Approx = true
		}

		if err := client.XAdd(ctx, args).Err(); err != nil {
			return fmt.Errorf("redis xadd: %w", err)
		}
	}

	return nil
}

func (d *Driver) Attach(ctx context.Context, queueID registry.ID, deliveries chan<- *queueapi.Delivery) (context.CancelFunc, error) {
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

	consumerName := fmt.Sprintf("%s-%s", queueID.String(), uuid.New().String()[:8])
	consumerCtx, cancel := context.WithCancel(ctx)

	// Main goroutine: read new messages via XREADGROUP
	go func() {
		for {
			select {
			case <-consumerCtx.Done():
				return
			case <-d.lifecycleCtxDone():
				return
			default:
			}

			streams, err := client.XReadGroup(consumerCtx, &goredis.XReadGroupArgs{
				Group:    consumerGroup,
				Consumer: consumerName,
				Streams:  []string{q.stream, ">"},
				Count:    10,
				Block:    blockTimeout,
			}).Result()
			if err != nil {
				if consumerCtx.Err() != nil {
					return
				}
				if errors.Is(err, goredis.Nil) {
					continue
				}
				d.logger.Error("redis xreadgroup error",
					zap.String("queue", queueID.String()),
					zap.Error(err))
				time.Sleep(time.Second)
				continue
			}

			for _, stream := range streams {
				for _, redisMsg := range stream.Messages {
					msg := parseRedisMessage(d.tc, queueCodec(q.opts), redisMsg)
					streamID := redisMsg.ID
					streamKey := q.stream

					delivery := &queueapi.Delivery{
						Message: msg,
						Ack: func(_ context.Context) error {
							return client.XAck(context.Background(), streamKey, consumerGroup, streamID).Err()
						},
						Nack: func(_ context.Context) error {
							// No XACK means the message will be redelivered via pending entries
							return nil
						},
					}

					select {
					case deliveries <- delivery:
					case <-consumerCtx.Done():
						return
					case <-d.lifecycleCtxDone():
						return
					}
				}
			}
		}
	}()

	// Claim goroutine: periodically recover pending messages from crashed consumers
	if d.claimInterval > 0 {
		go d.claimLoop(consumerCtx, queueID, q, client, consumerName, deliveries)
	}

	return cancel, nil
}

// claimLoop runs XAUTOCLAIM periodically to recover pending messages from
// crashed consumers. It delivers claimed messages through the same channel
// used by the main XREADGROUP loop.
func (d *Driver) claimLoop(
	ctx context.Context,
	queueID registry.ID,
	q *declaredQueue,
	client goredis.UniversalClient,
	consumerName string,
	deliveries chan<- *queueapi.Delivery,
) {
	ticker := time.NewTicker(d.claimInterval)
	defer ticker.Stop()

	// cursor tracks XAUTOCLAIM pagination across ticks; "0-0" starts from the
	// beginning of the PEL each time the cursor is exhausted.
	cursor := "0-0"

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.lifecycleCtxDone():
			return
		case <-ticker.C:
		}

		// XAUTOCLAIM claims messages idle longer than claimMinIdle and
		// transfers ownership to this consumer.
		claimed, newCursor, err := client.XAutoClaim(ctx, &goredis.XAutoClaimArgs{
			Stream:   q.stream,
			Group:    consumerGroup,
			Consumer: consumerName,
			MinIdle:  d.claimMinIdle,
			Start:    cursor,
			Count:    100,
		}).Result()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			d.logger.Warn("redis xautoclaim error",
				zap.String("queue", queueID.String()),
				zap.Error(err))
			continue
		}

		// Update cursor for next iteration; "0-0" means the PEL has been
		// fully scanned and we should start from the beginning next time.
		cursor = newCursor

		if len(claimed) == 0 {
			continue
		}

		d.logger.Debug("claimed pending messages",
			zap.String("queue", queueID.String()),
			zap.Int("count", len(claimed)))

		for _, redisMsg := range claimed {
			msg := parseRedisMessage(d.tc, queueCodec(q.opts), redisMsg)
			streamID := redisMsg.ID
			streamKey := q.stream

			delivery := &queueapi.Delivery{
				Message: msg,
				Ack: func(_ context.Context) error {
					return client.XAck(context.Background(), streamKey, consumerGroup, streamID).Err()
				},
				Nack: func(_ context.Context) error {
					return nil
				},
			}

			select {
			case deliveries <- delivery:
			case <-ctx.Done():
				return
			case <-d.lifecycleCtxDone():
				return
			}
		}
	}
}

func (d *Driver) DeclareQueue(ctx context.Context, queueID registry.ID, opts attrs.Attributes) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.queues[queueID]; exists {
		return nil
	}

	if d.client == nil {
		return queuesvc.ErrDriverNotStarted
	}

	stream := d.streamName(queueID, opts)

	// Create the consumer group (and stream if it doesn't exist)
	err := d.client.XGroupCreateMkStream(ctx, stream, consumerGroup, "0").Err()
	if err != nil && !isGroupExistsError(err) {
		return fmt.Errorf("redis xgroup create: %w", err)
	}

	d.queues[queueID] = &declaredQueue{
		stream: stream,
		opts:   opts,
	}

	d.logger.Debug("queue declared",
		zap.String("driver", d.id.String()),
		zap.String("queue", queueID.String()),
		zap.String("stream", stream))

	return nil
}

func (d *Driver) GetQueueInfo(ctx context.Context, queueID registry.ID) (attrs.Attributes, error) {
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

	xInfoResult, err := client.XInfoStream(ctx, q.stream).Result()
	if err != nil {
		return nil, fmt.Errorf("redis xinfo stream: %w", err)
	}

	groupInfo, err := client.XInfoGroups(ctx, q.stream).Result()
	if err != nil {
		return nil, fmt.Errorf("redis xinfo groups: %w", err)
	}

	consumerCount := 0
	for _, g := range groupInfo {
		if g.Name == consumerGroup {
			consumerCount = int(g.Consumers)
			break
		}
	}

	info := attrs.NewBag()
	info.Set(queueapi.StatsMessageCount, int(xInfoResult.Length))
	info.Set(queueapi.StatsReady, int(xInfoResult.Length))
	info.Set(queueapi.StatsConsumerCount, consumerCount)

	return info, nil
}

// neverClosedChan is a channel that never closes, used when driver is not started.
var neverClosedChan = make(chan struct{})

func (d *Driver) lifecycleCtxDone() <-chan struct{} {
	d.mu.RLock()
	ctx := d.ctx
	d.mu.RUnlock()
	if ctx != nil {
		return ctx.Done()
	}
	return neverClosedChan
}

func (d *Driver) Start(ctx context.Context) (<-chan any, error) {
	client := goredis.NewUniversalClient(d.opts)

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	d.mu.Lock()
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.client = client
	d.statusChan = make(chan any, 1)
	d.mu.Unlock()

	d.logger.Info("redis driver started",
		zap.String("id", d.id.String()),
		zap.Strings("addrs", d.opts.Addrs))

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

	d.logger.Info("redis driver stopped", zap.String("id", d.id.String()))
	return nil
}
