// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"context"
	"fmt"
	"math"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp091 "github.com/rabbitmq/amqp091-go"
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	amqpapi "github.com/wippyai/runtime/api/service/queue/amqp"
	queuesvc "github.com/wippyai/runtime/service/queue"
	"go.uber.org/zap"
)

// Driver option key constants under the "amqp" sub-bag of Config.DriverOptions
// (queue) and ConsumerOptions.DriverOptions (consumer).
const (
	// queue-declare options
	optionDurable     = "durable"
	optionAutoDelete  = "auto_delete"
	optionMessageTTL  = "message_ttl"
	optionQueueExpiry = "queue_expiry"
	optionMaxLength   = "max_length"

	// consumer options
	optionExclusive   = "exclusive"
	optionNoLocal     = "no_local"
	optionNoWait      = "no_wait"
	optionConsumerTag = "consumer_tag"

	// per-publish property keys on the "amqp.*" namespace
	publishPriority    = "amqp.priority"
	publishExpiration  = "amqp.expiration"
	publishXDelay      = "amqp.x_delay"
	publishContentEnc  = "amqp.content_encoding"
	publishDeliveryMod = "amqp.delivery_mode"
	publishMandatory   = "amqp.mandatory"
)

type declaredQueue struct {
	cfg  *queueapi.Config
	name string
}

// amqpAttachment is a live consumer subscription that must survive
// connection drops. The watcher re-declares queues on reconnect, but the
// amqp091 consume channel on the old connection is closed, so every
// active attachment has its own goroutine that reopens a channel and
// re-subscribes against the fresh connection.
type amqpAttachment struct {
	consumerCtx context.Context
	opts        *queueapi.ConsumerOptions
	deliveries  chan<- *queueapi.Delivery
	cancel      context.CancelFunc
	queueID     registry.ID
	codec       string
	name        string
}

// Driver implements the AMQP (RabbitMQ) queue driver.
type Driver struct {
	ctx         context.Context
	logger      *zap.Logger
	conn        *amqp091.Connection
	publishCh   *amqp091.Channel // persistent channel for Publish, lazily initialized
	cfg         *amqpapi.Config
	tc          payload.Transcoder
	queues      map[registry.ID]*declaredQueue
	attachments map[*amqpAttachment]struct{}
	cancel      context.CancelFunc
	statusChan  chan any
	id          registry.ID
	mu          sync.RWMutex
	pubMu       sync.Mutex // serializes publish operations on publishCh
}

// NewDriver creates a new AMQP driver instance.
func NewDriver(id registry.ID, cfg *amqpapi.Config, tc payload.Transcoder, logger *zap.Logger) *Driver {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Driver{
		id:          id,
		cfg:         cfg,
		tc:          tc,
		logger:      logger,
		queues:      make(map[registry.ID]*declaredQueue),
		attachments: make(map[*amqpAttachment]struct{}),
	}
}

// buildAMQPConfig constructs an amqp091.Config from the driver configuration.
func (d *Driver) buildAMQPConfig(ctx context.Context) (amqp091.Config, error) {
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
			return (&net.Dialer{Timeout: timeout}).Dial(network, addr)
		}
	}

	// Connection name in management UI
	if d.cfg.ConnectionName != "" {
		cfg.Properties = amqp091.NewConnectionProperties()
		cfg.Properties.SetClientConnectionName(d.cfg.ConnectionName)
	}

	// TLS
	if d.cfg.TLS != nil && d.cfg.TLS.Enabled {
		tlsCfg, err := d.cfg.TLS.BuildTLSConfig(ctx)
		if err != nil {
			return cfg, apierror.New(apierror.Internal, "amqp build tls config").WithCause(err)
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
			return cfg, apierror.New(apierror.Internal, "amqp parse url for auth").WithCause(err)
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
		return nil, apierror.New(apierror.Unavailable, "amqp channel").WithCause(err).WithRetryable(apierror.True)
	}
	return ch, nil
}

// getPublishChannel returns the persistent publish channel, creating one if
// needed or if the previous channel was closed. Takes the connection as an
// argument so callers can capture it under d.mu.RLock and hand it down: the
// pubMu critical section must never re-acquire d.mu, or it inverts against
// the reconnect path (which holds d.mu.Lock while waiting on pubMu).
// Caller must hold d.pubMu.
func (d *Driver) getPublishChannel(conn *amqp091.Connection) (*amqp091.Channel, error) {
	if d.publishCh != nil && !d.publishCh.IsClosed() {
		return d.publishCh, nil
	}

	ch, err := d.channelFromConn(conn)
	if err != nil {
		return nil, err
	}
	d.publishCh = ch
	return ch, nil
}

// queueName returns the broker-side queue name. cfg.QueueName wins; otherwise
// the registry ID name is used.
func queueName(queueID registry.ID, cfg *queueapi.Config) string {
	if cfg != nil && cfg.QueueName != "" {
		return cfg.QueueName
	}
	return queueID.Name
}

// deliverOrRelease hands the pooled Delivery off to the consumer channel,
// releasing the Message back to the pool if the send loses to ctx cancel or
// lifecycle shutdown. Returns true iff the send succeeded.
func deliverOrRelease(
	ctx context.Context,
	lifecycleDone <-chan struct{},
	deliveries chan<- *queueapi.Delivery,
	delivery *queueapi.Delivery,
) bool {
	select {
	case deliveries <- delivery:
		return true
	case <-ctx.Done():
		queueapi.ReleaseMessage(delivery.Message)
		return false
	case <-lifecycleDone:
		queueapi.ReleaseMessage(delivery.Message)
		return false
	}
}

// buildQueueArgs constructs the amqp091.Table arguments for QueueDeclare.
// Per-queue AMQP options (in Config.DriverOptions["amqp"]) win; the driver's
// DefaultQueueTTL / DefaultQueueExpiry serve as fallbacks.
func (d *Driver) buildQueueArgs(cfg *queueapi.Config) amqp091.Table {
	args := amqp091.Table{}

	ttlMs := d.cfg.DefaultQueueTTL.Milliseconds()
	expiryMs := d.cfg.DefaultQueueExpiry.Milliseconds()
	maxLen := 0

	if cfg != nil {
		bag := cfg.DriverBag("amqp")
		if s := bag.GetString(optionMessageTTL, ""); s != "" {
			if d, err := time.ParseDuration(s); err == nil {
				ttlMs = d.Milliseconds()
			}
		}
		if s := bag.GetString(optionQueueExpiry, ""); s != "" {
			if d, err := time.ParseDuration(s); err == nil {
				expiryMs = d.Milliseconds()
			}
		}
		maxLen = bag.GetInt(optionMaxLength, 0)
	}

	if ttlMs > 0 {
		args[amqp091.QueueMessageTTLArg] = clampMillisToInt32(ttlMs)
	}
	if expiryMs > 0 {
		args[amqp091.QueueTTLArg] = clampMillisToInt32(expiryMs)
	}
	if maxLen > 0 {
		args[amqp091.QueueMaxLenArg] = int32(maxLen)
	}

	if len(args) == 0 {
		return nil
	}
	return args
}

// clampMillisToInt32 clamps a millisecond value to the int32 range.
// AMQP 0-9-1 expects TTL and expiry as signed 32-bit integers.
func clampMillisToInt32(ms int64) int32 {
	if ms > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(ms)
}

func (d *Driver) Publish(ctx context.Context, queueID registry.ID, msgs ...*queueapi.Message) error {
	if !d.mu.TryRLock() {
		return apierror.New(apierror.Unavailable, "amqp driver busy (reconnecting)").WithRetryable(apierror.True)
	}
	q, exists := d.queues[queueID]
	conn := d.conn
	var cfg *queueapi.Config
	var name string
	if exists {
		cfg = q.cfg
		name = q.name
	}
	d.mu.RUnlock()

	if !exists {
		return queueapi.ErrQueueNotFound
	}

	d.pubMu.Lock()
	defer d.pubMu.Unlock()

	ch, err := d.getPublishChannel(conn)
	if err != nil {
		return err
	}

	codec := queueCodec(cfg)
	contentType := codecContentType(codec)

	for _, msg := range msgs {
		if msg.ID == "" {
			msg.ID = uuid.New().String()
		}

		body, err := marshalBody(d.tc, codec, msg.Body)
		if err != nil {
			return apierror.New(apierror.Internal, "amqp marshal body").WithCause(err)
		}

		// Merge the caller's headers into a fresh bag so extractMandatory
		// and buildPublishing can scrub the "amqp.mandatory" flag without
		// mutating caller-owned state. The merge also provides the natural
		// insertion point for queue-level delivery defaults when that layer
		// lands (see queue canonicalization plan).
		effective := attrs.NewBag().Merge(msg.Headers)

		mandatory := extractMandatory(effective)
		publishing := buildPublishing(msg.ID, body, contentType, effective)
		applyDefaultMessageTTL(&publishing, d.cfg.DefaultMessageTTL)

		if err := ch.PublishWithContext(ctx, "", name, mandatory, false, publishing); err != nil {
			// Channel is likely dead; nil it out so next call creates a fresh one.
			d.publishCh = nil
			return apierror.New(apierror.Unavailable, "amqp publish").WithCause(err).WithRetryable(apierror.True)
		}
	}

	return nil
}

// codecContentType maps a payload codec to an AMQP content-type string.
func codecContentType(codec string) string {
	switch codec {
	case payload.MsgPack:
		return "application/msgpack"
	case payload.JSON, "":
		return "application/json"
	default:
		return "application/" + codec
	}
}

// applyDefaultMessageTTL populates Publishing.Expiration with the driver's
// DefaultMessageTTL when the caller did not set one. AMQP expects the value
// as a millisecond string.
func applyDefaultMessageTTL(pub *amqp091.Publishing, ttl time.Duration) {
	if pub.Expiration != "" || ttl <= 0 {
		return
	}
	pub.Expiration = strconv.FormatInt(ttl.Milliseconds(), 10)
}

// extractMandatory reads the "amqp.mandatory" publish flag and removes it
// from the header bag so it never leaks into Publishing.Headers. A missing
// key defaults to false. Accepts bool, "true"/"false" strings, and numeric
// 1/0 to be lenient with YAML/Lua-origin headers.
func extractMandatory(headers attrs.Bag) bool {
	v, ok := headers[publishMandatory]
	if !ok {
		return false
	}
	delete(headers, publishMandatory)

	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "true" || x == "1"
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	}
	return false
}

// buildPublishing routes merged headers into the amqp091.Publishing struct.
// Neutral keys (correlation_id, reply_to, ...) populate typed struct fields.
// "amqp.*"-prefixed keys populate driver-specific fields or broker headers
// (prefix stripped). Remaining keys pass through as Publishing.Headers.
func buildPublishing(id string, body []byte, contentType string, effective attrs.Bag) amqp091.Publishing {
	pub := amqp091.Publishing{
		MessageId:   id,
		Body:        body,
		ContentType: contentType,
		Headers:     amqp091.Table{},
	}

	for k, v := range effective {
		switch k {
		case queueapi.HeaderCorrelationID:
			pub.CorrelationId = toString(v)
		case queueapi.HeaderReplyTo:
			pub.ReplyTo = toString(v)
		case queueapi.HeaderMessageType:
			pub.Type = toString(v)
		case queueapi.HeaderContentType:
			pub.ContentType = toString(v)
		case queueapi.HeaderEncoding:
			pub.ContentEncoding = toString(v)
		case queueapi.HeaderTimestamp:
			if ts, ok := toTimestamp(v); ok {
				pub.Timestamp = ts
			}
		case publishPriority:
			if p, ok := toUint8(v); ok {
				pub.Priority = p
			}
		case publishExpiration:
			pub.Expiration = toString(v)
		case publishDeliveryMod:
			if m, ok := toUint8(v); ok {
				pub.DeliveryMode = m
			}
		case publishContentEnc:
			pub.ContentEncoding = toString(v)
		case publishXDelay:
			if ms, ok := toInt64(v); ok {
				pub.Headers["x-delay"] = int32(ms)
			}
		case publishMandatory:
			// Caller extracts this via extractMandatory before invoking
			// PublishWithContext. Skip here so the flag never materializes as
			// a broker header if the caller forgot to pre-extract.
		default:
			// Pass keys through verbatim. Stripping the "amqp." prefix here
			// would collapse "amqp.foo" and "foo" to the same wire name and
			// break round-trip fidelity; the prefix has meaning only for the
			// typed-field cases above.
			pub.Headers[k] = v
		}
	}

	if pub.DeliveryMode == 0 {
		pub.DeliveryMode = amqp091.Persistent
	}

	return pub
}

// applyDeliveryHeaders inverts buildPublishing: it copies typed AMQP
// Publishing fields back into neutral / driver-prefixed header keys so a
// publish → consume round-trip preserves the headers the publisher set.
// amqpMsg.Headers pass through verbatim after the typed fields are
// populated, so broker-added table keys are not lost.
func applyDeliveryHeaders(dst attrs.Bag, amqpMsg amqp091.Delivery) {
	if amqpMsg.CorrelationId != "" {
		dst.Set(queueapi.HeaderCorrelationID, amqpMsg.CorrelationId)
	}
	if amqpMsg.ReplyTo != "" {
		dst.Set(queueapi.HeaderReplyTo, amqpMsg.ReplyTo)
	}
	if amqpMsg.ContentType != "" {
		dst.Set(queueapi.HeaderContentType, amqpMsg.ContentType)
	}
	if amqpMsg.ContentEncoding != "" {
		dst.Set(queueapi.HeaderEncoding, amqpMsg.ContentEncoding)
	}
	if amqpMsg.Type != "" {
		dst.Set(queueapi.HeaderMessageType, amqpMsg.Type)
	}
	if !amqpMsg.Timestamp.IsZero() {
		dst.Set(queueapi.HeaderTimestamp, amqpMsg.Timestamp.Unix())
	}
	// Priority and DeliveryMode are always stamped by the broker (0 is a
	// valid priority; the wire default for DeliveryMode is Transient). Elide
	// them and a publisher who set priority=0 can't read it back.
	dst.Set(publishPriority, int(amqpMsg.Priority))
	dst.Set(publishDeliveryMod, int(amqpMsg.DeliveryMode))
	if amqpMsg.Expiration != "" {
		dst.Set(publishExpiration, amqpMsg.Expiration)
	}
	// Broker-table headers pass through verbatim — buildPublishing writes
	// them verbatim too, so a publisher's "amqp.foo" and "foo" keys stay
	// distinct across the round-trip.
	for k, v := range amqpMsg.Headers {
		dst.Set(k, v)
	}
}

func toString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toUint8(v any) (uint8, bool) {
	switch n := v.(type) {
	case uint8:
		return n, true
	case int:
		if n < 0 || n > math.MaxUint8 {
			return 0, false
		}
		return uint8(n), true
	case int64:
		if n < 0 || n > math.MaxUint8 {
			return 0, false
		}
		return uint8(n), true
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > math.MaxUint8 {
			return 0, false
		}
		return uint8(n), true
	}
	return 0, false
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || n < math.MinInt64 || n > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	}
	return 0, false
}

func toTimestamp(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	case int64:
		return time.Unix(t, 0), true
	case int:
		return time.Unix(int64(t), 0), true
	}
	return time.Time{}, false
}

func (d *Driver) Attach(ctx context.Context, queueID registry.ID, opts *queueapi.ConsumerOptions, deliveries chan<- *queueapi.Delivery) (context.CancelFunc, error) {
	if !d.mu.TryRLock() {
		return nil, apierror.New(apierror.Unavailable, "amqp driver busy (reconnecting)").WithRetryable(apierror.True)
	}
	q, exists := d.queues[queueID]
	var cfg *queueapi.Config
	var name string
	if exists {
		cfg = q.cfg
		name = q.name
	}
	d.mu.RUnlock()

	if !exists {
		return nil, queueapi.ErrQueueNotFound
	}

	// Snapshot of the queue's codec for this consumer: redeclares update
	// future publishes and new attaches, but an in-flight consume loop
	// keeps the cfg it was started with.
	codec := queueCodec(cfg)

	// Validate the attachment once against the live connection so an
	// immediately-broken queue (wrong name, QoS rejected) surfaces as an
	// Attach error rather than an unreported background-loop failure.
	probeCh, err := d.openConsumerChannel(opts)
	if err != nil {
		return nil, err
	}
	probeCh.Close()

	consumerCtx, cancel := context.WithCancel(ctx)

	att := &amqpAttachment{
		queueID:     queueID,
		opts:        opts,
		deliveries:  deliveries,
		codec:       codec,
		name:        name,
		consumerCtx: consumerCtx,
		cancel:      cancel,
	}

	d.mu.Lock()
	d.attachments[att] = struct{}{}
	d.mu.Unlock()

	go d.runAttachment(att)

	return func() {
		cancel()
		d.mu.Lock()
		delete(d.attachments, att)
		d.mu.Unlock()
	}, nil
}

// openConsumerChannel opens a channel on the current connection, applies
// QoS, and returns it ready for a Consume call. Used both by Attach for
// the initial validation probe and by the re-attach loop after
// reconnect.
func (d *Driver) openConsumerChannel(opts *queueapi.ConsumerOptions) (*amqp091.Channel, error) {
	ch, err := d.getChannel()
	if err != nil {
		return nil, err
	}

	prefetch := d.cfg.PrefetchCount
	if opts != nil && opts.Prefetch > 0 {
		prefetch = opts.Prefetch
	}
	if prefetch > 0 {
		if err := ch.Qos(prefetch, 0, false); err != nil {
			ch.Close()
			return nil, apierror.New(apierror.Unavailable, "amqp qos").WithCause(err).WithRetryable(apierror.True)
		}
	}
	return ch, nil
}

// subscribe calls Consume on the provided channel using the attachment's
// consumer-scoped options.
func subscribe(ch *amqp091.Channel, name string, opts *queueapi.ConsumerOptions) (<-chan amqp091.Delivery, error) {
	autoAck := false
	exclusive := false
	noLocal := false
	noWait := false
	consumerTag := fmt.Sprintf("%s-%s", name, uuid.New().String()[:8])

	if opts != nil {
		autoAck = opts.AutoAck
		drvBag := opts.DriverBag("amqp")
		exclusive = drvBag.GetBool(optionExclusive, false)
		noLocal = drvBag.GetBool(optionNoLocal, false)
		noWait = drvBag.GetBool(optionNoWait, false)
		if tag := drvBag.GetString(optionConsumerTag, ""); tag != "" {
			consumerTag = fmt.Sprintf("%s-%s", tag, uuid.New().String()[:8])
		}
	}

	return ch.Consume(name, consumerTag, autoAck, exclusive, noLocal, noWait, nil)
}

// runAttachment keeps the attachment's consume loop alive across broker
// reconnects. On a fresh or restored connection it opens a channel and
// subscribes; when the subscription ends because the amqp091 deliveries
// channel closed (connection drop), it loops back and re-subscribes as
// soon as a live connection is available. Exits only on consumer cancel
// or driver lifecycle shutdown.
func (d *Driver) runAttachment(att *amqpAttachment) {
	// Backoff window used when the connection is down or a fresh
	// subscribe fails; matches the watcher's reconnect cadence so we
	// don't thrash before the broker is back.
	delay := d.cfg.ReconnectDelay
	if delay <= 0 {
		delay = 100 * time.Millisecond
	}

	for {
		select {
		case <-att.consumerCtx.Done():
			return
		case <-d.lifecycleCtxDone():
			return
		default:
		}

		ch, err := d.openConsumerChannel(att.opts)
		if err != nil {
			if !att.wait(d, delay) {
				return
			}
			continue
		}

		amqpDeliveries, err := subscribe(ch, att.name, att.opts)
		if err != nil {
			ch.Close()
			d.logger.Warn("amqp consume subscribe failed, retrying",
				zap.String("queue", att.queueID.String()),
				zap.Error(err))
			if !att.wait(d, delay) {
				return
			}
			continue
		}

		d.consumeLoop(ch, amqpDeliveries, att)
	}
}

// consumeLoop runs until the consumer is cancelled, the lifecycle ends,
// or the amqp091 deliveries channel closes (which happens when the
// underlying connection drops). The outer runAttachment loop picks up
// from here and re-subscribes on the new connection.
func (d *Driver) consumeLoop(ch *amqp091.Channel, amqpDeliveries <-chan amqp091.Delivery, att *amqpAttachment) {
	defer ch.Close()
	for {
		select {
		case <-att.consumerCtx.Done():
			return
		case <-d.lifecycleCtxDone():
			return
		case amqpMsg, ok := <-amqpDeliveries:
			if !ok {
				return
			}

			msg := queueapi.AcquireMessageWithID(
				amqpMsg.MessageId,
				payload.NewPayload(amqpMsg.Body, att.codec),
			)
			applyDeliveryHeaders(msg.Headers, amqpMsg)

			// Honor the caller's ctx on settle: a cancelled ctx (Lua
			// handler deadline hit, consumer worker shutdown) must not
			// drive a blocking broker-side Ack/Nack that could hang
			// behind channel-level backpressure.
			delivery := &queueapi.Delivery{
				Message: msg,
				Ack: func(ctx context.Context) error {
					if err := ctx.Err(); err != nil {
						return err
					}
					return amqpMsg.Ack(false)
				},
				Nack: func(ctx context.Context) error {
					if err := ctx.Err(); err != nil {
						return err
					}
					return amqpMsg.Nack(false, true)
				},
			}

			if !deliverOrRelease(att.consumerCtx, d.lifecycleCtxDone(), att.deliveries, delivery) {
				return
			}
		}
	}
}

// wait blocks the re-attach loop briefly between attempts, bailing out
// immediately if the consumer or driver is shutting down. Returns false
// if the loop should exit.
func (a *amqpAttachment) wait(d *Driver, delay time.Duration) bool {
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-a.consumerCtx.Done():
		return false
	case <-d.lifecycleCtxDone():
		return false
	}
}

func (d *Driver) DeclareQueue(_ context.Context, queueID registry.ID, cfg *queueapi.Config) error {
	if err := queuesvc.ValidateCodec(d.tc, queueCodec(cfg)); err != nil {
		return err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Redeclare updates the stored cfg so subsequent Publish / Attach see
	// the new options. AMQP does not allow a broker-side QueueDeclare with
	// different args on an existing queue (that's a 406 channel error), so
	// we do not re-call ch.QueueDeclare here — the cfg update covers
	// publish-time / consume-time driver behavior.
	if existing, exists := d.queues[queueID]; exists {
		existing.cfg = cfg
		return nil
	}

	name := queueName(queueID, cfg)

	durable := true
	autoDelete := false
	if cfg != nil {
		bag := cfg.DriverBag("amqp")
		durable = bag.GetBool(optionDurable, true)
		autoDelete = bag.GetBool(optionAutoDelete, false)
	}

	ch, err := d.getChannelLocked()
	if err != nil {
		return err
	}
	defer ch.Close()

	args := d.buildQueueArgs(cfg)

	_, err = ch.QueueDeclare(
		name,       // name
		durable,    // durable
		autoDelete, // auto-delete
		false,      // exclusive
		false,      // no-wait
		args,       // args
	)
	if err != nil {
		return apierror.New(apierror.Unavailable, "amqp declare queue").WithCause(err).WithRetryable(apierror.True)
	}

	d.queues[queueID] = &declaredQueue{
		name: name,
		cfg:  cfg,
	}

	d.logger.Debug("queue declared",
		zap.String("driver", d.id.String()),
		zap.String("queue", queueID.String()),
		zap.String("amqp_name", name),
		zap.Bool("durable", durable))

	return nil
}

func (d *Driver) GetQueueInfo(_ context.Context, queueID registry.ID) (attrs.Attributes, error) {
	if !d.mu.TryRLock() {
		return nil, apierror.New(apierror.Unavailable, "amqp driver busy (reconnecting)").WithRetryable(apierror.True)
	}
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
		return nil, apierror.New(apierror.Unavailable, "amqp queue inspect").WithCause(err).WithRetryable(apierror.True)
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
	amqpCfg, err := d.buildAMQPConfig(ctx)
	if err != nil {
		return nil, apierror.New(apierror.Internal, "amqp config").WithCause(err)
	}

	conn, err := amqp091.DialConfig(d.cfg.URL, amqpCfg)
	if err != nil {
		return nil, apierror.New(apierror.Unavailable, "amqp dial").WithCause(err).WithRetryable(apierror.True)
	}

	d.mu.Lock()
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.conn = conn
	d.statusChan = make(chan any, 1)
	d.mu.Unlock()

	// Start connection watcher for automatic reconnection
	if d.cfg.ReconnectDelay > 0 {
		go d.watchConnection(amqpCfg)
	}

	d.logger.Info("amqp driver started",
		zap.String("id", d.id.String()),
		zap.String("url", sanitizeURL(d.cfg.URL)))

	return d.statusChan, nil
}

// watchConnection monitors the AMQP connection via NotifyClose and reconnects
// with exponential backoff when the connection drops. After reconnecting, it
// re-declares all previously declared queues and invalidates the publish channel.
func (d *Driver) watchConnection(amqpCfg amqp091.Config) {
	for {
		d.mu.RLock()
		conn := d.conn
		ctx := d.ctx
		d.mu.RUnlock()

		if ctx == nil || ctx.Err() != nil {
			return
		}
		if conn == nil {
			return
		}

		// NotifyClose returns a channel that receives an error when the
		// connection is closed (server-initiated or network failure).
		closeCh := conn.NotifyClose(make(chan *amqp091.Error, 1))

		select {
		case <-ctx.Done():
			return
		case amqpErr, ok := <-closeCh:
			if !ok {
				// Channel closed without error — likely a graceful shutdown.
				return
			}
			d.logger.Warn("amqp connection lost, reconnecting...",
				zap.String("id", d.id.String()),
				zap.Error(amqpErr))
		}

		// Reconnect with exponential backoff. Use the ctx snapshot taken
		// at the top of the outer loop so the backoff never re-reads
		// d.ctx unlocked — a restart (new Start after Stop) would
		// otherwise race the reassignment.
		delay := d.cfg.ReconnectDelay
		for {
			if ctx.Err() != nil {
				return
			}

			d.logger.Info("amqp reconnect attempt",
				zap.String("id", d.id.String()),
				zap.Duration("delay", delay))

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return
			}

			newConn, err := amqp091.DialConfig(d.cfg.URL, amqpCfg)
			if err != nil {
				d.logger.Warn("amqp reconnect failed",
					zap.String("id", d.id.String()),
					zap.Error(err))
				delay *= 2
				if delay > d.cfg.ReconnectMaxDelay {
					delay = d.cfg.ReconnectMaxDelay
				}
				continue
			}

			// Reconnected — swap connection under write lock.
			// The write lock blocks all TryRLock callers in Publish/Attach/GetQueueInfo,
			// which return "driver busy (reconnecting)" while we're holding it.
			d.mu.Lock()

			// Invalidate publish channel
			d.pubMu.Lock()
			if d.publishCh != nil && !d.publishCh.IsClosed() {
				d.publishCh.Close()
			}
			d.publishCh = nil
			d.pubMu.Unlock()

			d.conn = newConn

			// Re-declare all queues on the new connection
			d.redeclareQueuesLocked()

			d.mu.Unlock()

			d.logger.Info("amqp reconnected successfully",
				zap.String("id", d.id.String()))

			break // exit backoff loop, resume watching
		}
	}
}

// redeclareQueuesLocked re-declares all known queues on the current connection.
// Each queue gets a fresh AMQP channel: a channel exception on one queue
// (typically PRECONDITION_FAILED when a recorded cfg drifted from the
// broker-side definition) closes the channel, and sharing the channel
// would turn that one error into "channel/connection is not open"
// failures for every queue later in the iteration order.
// Caller must hold d.mu write lock.
func (d *Driver) redeclareQueuesLocked() {
	for queueID, q := range d.queues {
		durable := true
		autoDelete := false
		if q.cfg != nil {
			bag := q.cfg.DriverBag("amqp")
			durable = bag.GetBool(optionDurable, true)
			autoDelete = bag.GetBool(optionAutoDelete, false)
		}

		args := d.buildQueueArgs(q.cfg)

		ch, err := d.channelFromConn(d.conn)
		if err != nil {
			d.logger.Error("amqp redeclare: failed to open channel",
				zap.String("queue", queueID.String()),
				zap.Error(err))
			continue
		}

		_, err = ch.QueueDeclare(q.name, durable, autoDelete, false, false, args)
		ch.Close()
		if err != nil {
			d.logger.Error("amqp redeclare queue failed",
				zap.String("queue", queueID.String()),
				zap.String("amqp_name", q.name),
				zap.Error(err))
		} else {
			d.logger.Debug("amqp redeclared queue",
				zap.String("queue", queueID.String()),
				zap.String("amqp_name", q.name))
		}
	}
}

func (d *Driver) Stop(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cancel != nil {
		d.cancel()
	}

	// Close the persistent publish channel.
	d.pubMu.Lock()
	if d.publishCh != nil && !d.publishCh.IsClosed() {
		d.publishCh.Close()
	}
	d.publishCh = nil
	d.pubMu.Unlock()

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
