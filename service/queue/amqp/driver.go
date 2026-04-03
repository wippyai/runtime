// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp091 "github.com/rabbitmq/amqp091-go"
	"github.com/wippyai/runtime/api/attrs"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	amqpapi "github.com/wippyai/runtime/api/service/queue/amqp"
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
	cfg        *amqpapi.Config
	queues     map[registry.ID]*declaredQueue
	cancel     context.CancelFunc
	statusChan chan any
	id         registry.ID
	mu         sync.RWMutex
}

// NewDriver creates a new AMQP driver instance.
func NewDriver(id registry.ID, cfg *amqpapi.Config, logger *zap.Logger) *Driver {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Driver{
		id:     id,
		cfg:    cfg,
		logger: logger,
		queues: make(map[registry.ID]*declaredQueue),
	}
}

// buildAMQPConfig constructs an amqp091.Config from the driver configuration.
func (d *Driver) buildAMQPConfig() (amqp091.Config, error) {
	cfg := amqp091.Config{
		Locale: "en_US",
	}

	if d.cfg.Vhost != "" {
		cfg.Vhost = d.cfg.Vhost
	}
	if d.cfg.ChannelMax != 0 {
		cfg.ChannelMax = d.cfg.ChannelMax
	}
	if d.cfg.FrameSize != 0 {
		cfg.FrameSize = d.cfg.FrameSize
	}
	if d.cfg.Heartbeat != 0 {
		cfg.Heartbeat = d.cfg.Heartbeat
	}

	// Connection timeout via custom dialer
	if d.cfg.ConnectionTimeout != 0 {
		timeout := d.cfg.ConnectionTimeout
		cfg.Dial = func(network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: timeout}
			conn, err := dialer.Dial(network, addr)
			if err != nil {
				return nil, err
			}
			if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
				return nil, err
			}
			return conn, nil
		}
	}

	// Connection name in management UI
	if d.cfg.ConnectionName != "" {
		cfg.Properties = amqp091.NewConnectionProperties()
		cfg.Properties.SetClientConnectionName(d.cfg.ConnectionName)
	}

	// TLS
	if d.cfg.TLS != nil && d.cfg.TLS.Enabled {
		tlsCfg, err := d.cfg.TLS.BuildTLSConfig()
		if err != nil {
			return cfg, fmt.Errorf("amqp build tls config: %w", err)
		}
		cfg.TLSClientConfig = tlsCfg
	}

	// SASL authentication mechanism
	switch d.cfg.AuthMechanism {
	case "EXTERNAL":
		cfg.SASL = []amqp091.Authentication{&amqp091.ExternalAuth{}}
	case "AMQPLAIN":
		// Parse credentials from URL for AMQPlain
		uri, err := amqp091.ParseURI(d.cfg.URL)
		if err != nil {
			return cfg, fmt.Errorf("amqp parse url for auth: %w", err)
		}
		cfg.SASL = []amqp091.Authentication{uri.AMQPlainAuth()}
	case "PLAIN", "":
		// Default: let the library extract PlainAuth from URL
	}

	return cfg, nil
}

func (d *Driver) getChannel() (*amqp091.Channel, error) {
	d.mu.RLock()
	conn := d.conn
	d.mu.RUnlock()
	return d.channelFromConn(conn)
}

// getChannelLocked returns a channel using the connection already held under lock.
func (d *Driver) getChannelLocked() (*amqp091.Channel, error) {
	return d.channelFromConn(d.conn)
}

func (d *Driver) channelFromConn(conn *amqp091.Connection) (*amqp091.Channel, error) {
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

// messageExpiration returns the AMQP Expiration string for a message.
// It checks HeaderTTL (seconds) from message headers first, then falls back
// to the driver's DefaultMessageTTL config. Returns "" for no expiration.
func (d *Driver) messageExpiration(msg *queueapi.Message) string {
	// Per-message TTL from headers takes priority
	if msg.Headers != nil {
		if ttlSec := msg.Headers.GetInt(queueapi.HeaderTTL, 0); ttlSec > 0 {
			return fmt.Sprintf("%d", ttlSec*1000) // seconds → milliseconds
		}
	}
	// Fall back to default config TTL
	if d.cfg.DefaultMessageTTL > 0 {
		return fmt.Sprintf("%d", d.cfg.DefaultMessageTTL.Milliseconds())
	}
	return ""
}

// buildQueueArgs constructs the amqp091.Table arguments for QueueDeclare
// based on driver config defaults. Returns nil if no arguments are needed.
func (d *Driver) buildQueueArgs() amqp091.Table {
	hasTTL := d.cfg.DefaultQueueTTL > 0
	hasExpiry := d.cfg.DefaultQueueExpiry > 0

	if !hasTTL && !hasExpiry {
		return nil
	}

	args := amqp091.Table{}
	if hasTTL {
		args[amqp091.QueueMessageTTLArg] = int32(d.cfg.DefaultQueueTTL.Milliseconds())
	}
	if hasExpiry {
		args[amqp091.QueueTTLArg] = int32(d.cfg.DefaultQueueExpiry.Milliseconds())
	}

	return args
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
			Expiration:  d.messageExpiration(msg),
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

	// Set QoS (prefetch) if configured
	if d.cfg.PrefetchCount > 0 {
		if err := ch.Qos(d.cfg.PrefetchCount, 0, false); err != nil {
			ch.Close()
			return nil, fmt.Errorf("amqp qos: %w", err)
		}
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

	ch, err := d.getChannelLocked()
	if err != nil {
		return err
	}
	defer ch.Close()

	args := d.buildQueueArgs()

	_, err = ch.QueueDeclare(
		name,    // name
		durable, // durable
		false,   // auto-delete
		false,   // exclusive
		false,   // no-wait
		args,    // args
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
	amqpCfg, err := d.buildAMQPConfig()
	if err != nil {
		return nil, fmt.Errorf("amqp config: %w", err)
	}

	conn, err := amqp091.DialConfig(d.cfg.URL, amqpCfg)
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
		zap.String("url", sanitizeURL(d.cfg.URL)))

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
