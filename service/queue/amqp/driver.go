// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	amqp091 "github.com/rabbitmq/amqp091-go"
	"github.com/wippyai/runtime/api/attrs"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	queuesvc "github.com/wippyai/runtime/service/queue"
	"go.uber.org/zap"
)

type declaredQueue struct {
	opts attrs.Attributes
	name string
}

// Driver implements the AMQP (RabbitMQ) queue driver.
type Driver struct {
	ctx        context.Context
	logger     *zap.Logger
	conn       *amqp091.Connection
	url        string
	queues     map[registry.ID]*declaredQueue
	cancel     context.CancelFunc
	statusChan chan any
	id         registry.ID
	mu         sync.RWMutex
}

// NewDriver creates a new AMQP driver instance.
func NewDriver(id registry.ID, url string, logger *zap.Logger) *Driver {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Driver{
		id:     id,
		url:    url,
		logger: logger,
		queues: make(map[registry.ID]*declaredQueue),
	}
}

func (d *Driver) getChannel() (*amqp091.Channel, error) {
	d.mu.RLock()
	conn := d.conn
	d.mu.RUnlock()

	if conn == nil || conn.IsClosed() {
		return nil, queuesvc.ErrDriverNotStarted
	}

	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("amqp channel: %w", err)
	}
	return ch, nil
}

func (d *Driver) queueName(queueID registry.ID, opts attrs.Attributes) string {
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
	d.mu.RUnlock()

	if !exists {
		return queueapi.ErrQueueNotFound
	}

	ch, err := d.getChannel()
	if err != nil {
		return err
	}
	defer ch.Close()

	for _, msg := range msgs {
		if msg.ID == "" {
			msg.ID = uuid.New().String()
		}

		headers := amqp091.Table{}
		if msg.Headers != nil {
			for k, v := range msg.Headers {
				headers[k] = v
			}
		}

		body, err := marshalBody(msg.Body)
		if err != nil {
			return fmt.Errorf("amqp marshal body: %w", err)
		}

		publishing := amqp091.Publishing{
			MessageId:   msg.ID,
			Headers:     headers,
			ContentType: "application/json",
			Body:        body,
		}

		if err := ch.PublishWithContext(ctx, "", q.name, false, false, publishing); err != nil {
			return fmt.Errorf("amqp publish: %w", err)
		}
	}

	return nil
}

func (d *Driver) Attach(ctx context.Context, queueID registry.ID, deliveries chan<- *queueapi.Delivery) (context.CancelFunc, error) {
	d.mu.RLock()
	q, exists := d.queues[queueID]
	d.mu.RUnlock()

	if !exists {
		return nil, queueapi.ErrQueueNotFound
	}

	ch, err := d.getChannel()
	if err != nil {
		return nil, err
	}

	consumerTag := fmt.Sprintf("%s-%s", queueID.String(), uuid.New().String()[:8])
	amqpDeliveries, err := ch.Consume(
		q.name,      // queue
		consumerTag, // consumer tag
		false,       // auto-ack
		false,       // exclusive
		false,       // no-local
		false,       // no-wait
		nil,         // args
	)
	if err != nil {
		ch.Close()
		return nil, fmt.Errorf("amqp consume: %w", err)
	}

	consumerCtx, cancel := context.WithCancel(ctx)

	go func() {
		defer ch.Close()
		for {
			select {
			case <-consumerCtx.Done():
				return
			case <-d.lifecycleCtxDone():
				return
			case amqpMsg, ok := <-amqpDeliveries:
				if !ok {
					return
				}

				msg := &queueapi.Message{
					ID:      amqpMsg.MessageId,
					Body:    unmarshalBody(amqpMsg.Body),
					Headers: attrs.NewBag(),
				}

				for k, v := range amqpMsg.Headers {
					msg.Headers.Set(k, v)
				}

				delivery := &queueapi.Delivery{
					Message: msg,
					Ack: func(_ context.Context) error {
						return amqpMsg.Ack(false)
					},
					Nack: func(_ context.Context) error {
						return amqpMsg.Nack(false, true)
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
	}()

	return cancel, nil
}

func (d *Driver) DeclareQueue(_ context.Context, queueID registry.ID, opts attrs.Attributes) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.queues[queueID]; exists {
		return nil
	}

	name := d.queueName(queueID, opts)
	durable := true
	if opts != nil {
		if v := opts.GetString(queueapi.OptionDurable, ""); v == "false" {
			durable = false
		}
	}

	ch, err := d.getChannel()
	if err != nil {
		return err
	}
	defer ch.Close()

	_, err = ch.QueueDeclare(
		name,    // name
		durable, // durable
		false,   // auto-delete
		false,   // exclusive
		false,   // no-wait
		nil,     // args
	)
	if err != nil {
		return fmt.Errorf("amqp declare queue: %w", err)
	}

	d.queues[queueID] = &declaredQueue{
		name: name,
		opts: opts,
	}

	d.logger.Debug("queue declared",
		zap.String("driver", d.id.String()),
		zap.String("queue", queueID.String()),
		zap.String("amqp_name", name),
		zap.Bool("durable", durable))

	return nil
}

func (d *Driver) GetQueueInfo(_ context.Context, queueID registry.ID) (attrs.Attributes, error) {
	d.mu.RLock()
	q, exists := d.queues[queueID]
	d.mu.RUnlock()

	if !exists {
		return nil, queueapi.ErrQueueNotFound
	}

	ch, err := d.getChannel()
	if err != nil {
		return nil, err
	}
	defer ch.Close()

	qi, err := ch.QueueDeclarePassive(q.name, false, false, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("amqp queue inspect: %w", err)
	}

	info := attrs.NewBag()
	info.Set(queueapi.StatsMessageCount, qi.Messages)
	info.Set(queueapi.StatsConsumerCount, qi.Consumers)
	info.Set(queueapi.StatsReady, qi.Messages)

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
	conn, err := amqp091.Dial(d.url)
	if err != nil {
		return nil, fmt.Errorf("amqp dial: %w", err)
	}

	d.mu.Lock()
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.conn = conn
	d.statusChan = make(chan any, 1)
	d.mu.Unlock()

	d.logger.Info("amqp driver started",
		zap.String("id", d.id.String()),
		zap.String("url", sanitizeURL(d.url)))

	return d.statusChan, nil
}

func (d *Driver) Stop(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cancel != nil {
		d.cancel()
	}

	if d.conn != nil && !d.conn.IsClosed() {
		d.conn.Close()
	}

	d.queues = make(map[registry.ID]*declaredQueue)

	if d.statusChan != nil {
		close(d.statusChan)
	}

	d.logger.Info("amqp driver stopped", zap.String("id", d.id.String()))
	return nil
}
