// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/google/uuid"
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	sqsapi "github.com/wippyai/runtime/api/service/aws/sqs"
	queuesvc "github.com/wippyai/runtime/service/queue"
	"go.uber.org/zap"
)

// Driver option keys under the "sqs" sub-bag of the respective DriverOptions
// attrs.Bag. Consumer-scoped and queue-scoped keys are deliberately distinct
// so a single "visibility_timeout" entry can't silently mean two things.
const (
	// consumer-side receive tunables (ConsumerOptions.DriverOptions["sqs"])
	optionVisibilityTimeout = "visibility_timeout"
	optionWaitTime          = "wait_time"

	// queue-side declare-time defaults (Config.DriverOptions["sqs"])
	optionQueueDefaultVisibilityTimeout = "default_visibility_timeout"

	// per-publish keys on the "sqs.*" namespace — typed on SendMessage
	publishDelaySeconds = "sqs.delay_seconds"
	publishMessageGroup = "sqs.message_group_id"
	publishDedupID      = "sqs.message_deduplication_id"
)

type declaredQueue struct {
	cfg *queueapi.Config
	url string
}

// Driver implements the AWS SQS queue driver.
type Driver struct {
	ctx        context.Context
	logger     *zap.Logger
	client     *awssqs.Client
	cfg        *sqsapi.Config
	tc         payload.Transcoder
	queues     map[registry.ID]*declaredQueue
	cancel     context.CancelFunc
	statusChan chan any
	id         registry.ID
	awsCfg     aws.Config
	mu         sync.RWMutex
}

// NewDriver creates a new SQS driver instance.
//
// The SQS client is built at construction time. DeclareQueue (and every
// other one-shot RPC) uses the caller-supplied context, so it must not
// depend on Start() having completed — declarations fire during entry
// load, before the supervisor has started the driver service.
func NewDriver(id registry.ID, cfg *sqsapi.Config, awsCfg aws.Config, tc payload.Transcoder, logger *zap.Logger) *Driver {
	if logger == nil {
		logger = zap.NewNop()
	}
	client := awssqs.NewFromConfig(awsCfg, func(o *awssqs.Options) {
		if cfg.DisableMessageChecksumValidation {
			o.DisableMessageChecksumValidation = true
		}
		if cfg.UseFIPS {
			o.EndpointOptions.UseFIPSEndpoint = aws.FIPSEndpointStateEnabled
		}
		if cfg.UseDualStack {
			o.EndpointOptions.UseDualStackEndpoint = aws.DualStackEndpointStateEnabled
		}
	})
	return &Driver{
		id:     id,
		cfg:    cfg,
		awsCfg: awsCfg,
		client: client,
		tc:     tc,
		logger: logger,
		queues: make(map[registry.ID]*declaredQueue),
	}
}

func queueName(queueID registry.ID, cfg *queueapi.Config) string {
	if cfg != nil && cfg.QueueName != "" {
		return cfg.QueueName
	}
	return queueID.Name
}

// queueCodec returns the wire codec format for a queue, defaulting to JSON.
func queueCodec(cfg *queueapi.Config) string {
	if cfg != nil && cfg.Codec != "" {
		return cfg.Codec
	}
	return payload.JSON
}

// sqsBatchLimit is the maximum number of messages per SQS SendMessageBatch call.
const sqsBatchLimit = 10

func (d *Driver) Publish(ctx context.Context, queueID registry.ID, msgs ...*queueapi.Message) error {
	d.mu.RLock()
	q, exists := d.queues[queueID]
	client := d.client
	var cfg *queueapi.Config
	var url string
	if exists {
		cfg = q.cfg
		url = q.url
	}
	d.mu.RUnlock()

	if !exists {
		return queueapi.ErrQueueNotFound
	}
	if client == nil {
		return queuesvc.ErrDriverNotStarted
	}

	codec := queueCodec(cfg)
	fifo := isFIFOName(queueName(queueID, cfg))

	entries := make([]types.SendMessageBatchRequestEntry, 0, len(msgs))
	for i, msg := range msgs {
		if msg.ID == "" {
			msg.ID = uuid.New().String()
		}

		if fifo {
			if err := validateFIFOPublish(msg.Headers); err != nil {
				return err
			}
		}

		body, err := marshalBody(d.tc, codec, msg.Body)
		if err != nil {
			return apierror.New(apierror.Invalid, "sqs marshal body").WithCause(err).WithRetryable(apierror.False)
		}

		entry := types.SendMessageBatchRequestEntry{
			Id:          aws.String(strconv.Itoa(i)),
			MessageBody: aws.String(string(body)),
		}
		applyHeaders(&entry, msg.Headers)
		entries = append(entries, entry)
	}

	for start := 0; start < len(entries); start += sqsBatchLimit {
		end := start + sqsBatchLimit
		if end > len(entries) {
			end = len(entries)
		}
		chunk := entries[start:end]

		// Optimize: use SendMessage for single-message batches.
		if len(chunk) == 1 {
			input := &awssqs.SendMessageInput{
				QueueUrl:               aws.String(url),
				MessageBody:            chunk[0].MessageBody,
				MessageAttributes:      chunk[0].MessageAttributes,
				DelaySeconds:           chunk[0].DelaySeconds,
				MessageGroupId:         chunk[0].MessageGroupId,
				MessageDeduplicationId: chunk[0].MessageDeduplicationId,
			}
			if _, err := client.SendMessage(ctx, input); err != nil {
				return apierror.New(apierror.Unavailable, "sqs send message").WithCause(err).WithRetryable(apierror.True)
			}
			continue
		}

		result, err := client.SendMessageBatch(ctx, &awssqs.SendMessageBatchInput{
			QueueUrl: aws.String(url),
			Entries:  chunk,
		})
		if err != nil {
			return apierror.New(apierror.Unavailable, "sqs send message batch").WithCause(err).WithRetryable(apierror.True)
		}

		if len(result.Failed) > 0 {
			f := result.Failed[0]
			return apierror.New(apierror.Internal, fmt.Sprintf("sqs batch entry %s failed: [%s] %s",
				aws.ToString(f.Id), aws.ToString(f.Code), aws.ToString(f.Message))).WithRetryable(apierror.False)
		}
	}

	return nil
}

// applyHeaders routes merged headers into the SendMessage / batch entry fields.
// Neutral keys and sqs.*-prefixed keys populate typed fields; sqs.message_attributes.*
// keys (and everything else) become string MessageAttributes.
func applyHeaders(entry *types.SendMessageBatchRequestEntry, effective attrs.Bag) {
	msgAttrs := map[string]types.MessageAttributeValue{}
	for k, v := range effective {
		switch k {
		case publishDelaySeconds:
			if n, ok := toInt32(v); ok {
				entry.DelaySeconds = n
			}
		case publishMessageGroup:
			entry.MessageGroupId = aws.String(toString(v))
		case publishDedupID:
			entry.MessageDeduplicationId = aws.String(toString(v))
		default:
			// Keys are written to MessageAttributes verbatim — the full key
			// (including any "sqs.message_attributes." prefix) round-trips
			// unchanged to the consumer. The DataType is chosen to preserve
			// the Go type (int/float → Number, bool → String.bool,
			// []byte → Binary, everything else → String).
			msgAttrs[k] = typedAttr(v)
		}
	}
	if len(msgAttrs) > 0 {
		entry.MessageAttributes = msgAttrs
	}
}

// typedAttr picks the SQS DataType that preserves the Go type across a
// publish/receive round-trip. SQS supports three base DataTypes (String,
// Number, Binary); bool has no native equivalent, so we use the "String.bool"
// custom-type suffix AWS passes through verbatim.
func typedAttr(v any) types.MessageAttributeValue {
	switch x := v.(type) {
	case []byte:
		return types.MessageAttributeValue{
			DataType:    aws.String("Binary"),
			BinaryValue: x,
		}
	case bool:
		s := "false"
		if x {
			s = "true"
		}
		return types.MessageAttributeValue{
			DataType:    aws.String("String.bool"),
			StringValue: aws.String(s),
		}
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return types.MessageAttributeValue{
			DataType:    aws.String("Number"),
			StringValue: aws.String(toString(v)),
		}
	default:
		return types.MessageAttributeValue{
			DataType:    aws.String("String"),
			StringValue: aws.String(toString(v)),
		}
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

func toInt32(v any) (int32, bool) {
	switch n := v.(type) {
	case int:
		if int64(n) < math.MinInt32 || int64(n) > math.MaxInt32 {
			return 0, false
		}
		return int32(n), true
	case int32:
		return n, true
	case int64:
		if n < math.MinInt32 || n > math.MaxInt32 {
			return 0, false
		}
		return int32(n), true
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || n < math.MinInt32 || n > math.MaxInt32 {
			return 0, false
		}
		return int32(n), true
	}
	return 0, false
}

// sqsMaxBatchSize is the hard cap AWS imposes on ReceiveMessage batch size.
const sqsMaxBatchSize = 10

// isFIFOName reports whether the queue name has the ".fifo" suffix AWS
// requires for FIFO queues. The bare string "fifo" does not qualify — AWS
// enforces a dot-prefixed suffix.
func isFIFOName(name string) bool {
	const suffix = ".fifo"
	return len(name) > len(suffix) && name[len(name)-len(suffix):] == suffix
}

// validateFIFOPublish ensures the caller supplied the MessageGroupId header
// that FIFO queues require. Produces an apierror.Invalid so the manager can
// short-circuit before a round-trip to AWS.
func validateFIFOPublish(headers map[string]any) error {
	if v, ok := headers[publishMessageGroup]; ok {
		if s, ok := v.(string); ok && s != "" {
			return nil
		}
	}
	return apierror.New(apierror.Invalid,
		"sqs fifo publish missing "+publishMessageGroup).WithRetryable(apierror.False)
}

// waitWithContext blocks for d, returning false if ctx or lifecycleDone fires
// first. Used as a cancel-aware backoff after a transient ReceiveMessage
// error so the consumer goroutine exits promptly on shutdown.
func waitWithContext(ctx context.Context, lifecycleDone <-chan struct{}, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	case <-lifecycleDone:
		return false
	}
}

// ackClient is the subset of *awssqs.Client the ack/nack callbacks depend on.
// Defined so the callback builders can be unit-tested without standing up a
// real AWS client.
type ackClient interface {
	DeleteMessage(context.Context, *awssqs.DeleteMessageInput, ...func(*awssqs.Options)) (*awssqs.DeleteMessageOutput, error)
	ChangeMessageVisibility(context.Context, *awssqs.ChangeMessageVisibilityInput, ...func(*awssqs.Options)) (*awssqs.ChangeMessageVisibilityOutput, error)
}

func buildAck(c ackClient, queueURL, receiptHandle string) func(context.Context) error {
	return func(ctx context.Context) error {
		_, err := c.DeleteMessage(ctx, &awssqs.DeleteMessageInput{
			QueueUrl:      aws.String(queueURL),
			ReceiptHandle: aws.String(receiptHandle),
		})
		if err != nil {
			return apierror.New(apierror.Unavailable, "sqs delete message").WithCause(err).WithRetryable(apierror.True)
		}
		return nil
	}
}

func buildNack(c ackClient, queueURL, receiptHandle string) func(context.Context) error {
	return func(ctx context.Context) error {
		_, err := c.ChangeMessageVisibility(ctx, &awssqs.ChangeMessageVisibilityInput{
			QueueUrl:          aws.String(queueURL),
			ReceiptHandle:     aws.String(receiptHandle),
			VisibilityTimeout: 0,
		})
		if err != nil {
			return apierror.New(apierror.Unavailable, "sqs change message visibility").WithCause(err).WithRetryable(apierror.True)
		}
		return nil
	}
}

// deliverOrRelease tries to hand the pooled Delivery off to the consumer
// channel. When the send loses the race to ctx cancellation or lifecycle
// shutdown, the Message must be returned to the pool — the consumer never
// received it, so it can't release it. Returns true iff the send succeeded.
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

// applyReceivedAttributes reverses applyHeaders: it reads an SQS
// ReceiveMessage result's MessageAttributes verbatim and lifts system
// Attributes (MessageGroupId, MessageDeduplicationId) back under the same
// "sqs.*" keys the publisher used, so a round-trip preserves the full key
// set without ambiguity.
func applyReceivedAttributes(sqsMsg types.Message) attrs.Bag {
	headers := attrs.NewBag()

	for k, v := range sqsMsg.MessageAttributes {
		headers.Set(k, decodeAttr(v))
	}

	if v, ok := sqsMsg.Attributes["MessageGroupId"]; ok && v != "" {
		headers.Set(publishMessageGroup, v)
	}
	if v, ok := sqsMsg.Attributes["MessageDeduplicationId"]; ok && v != "" {
		headers.Set(publishDedupID, v)
	}

	return headers
}

// decodeAttr restores the Go type of a MessageAttributeValue based on its
// DataType, mirroring typedAttr on the publish side. Unknown custom-typed
// strings fall back to the raw StringValue so nothing is silently dropped.
func decodeAttr(v types.MessageAttributeValue) any {
	dt := aws.ToString(v.DataType)

	if len(v.BinaryValue) > 0 && (dt == "Binary" || dt == "") {
		return v.BinaryValue
	}

	s := aws.ToString(v.StringValue)

	switch dt {
	case "Number":
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
		return s
	case "String.bool":
		return s == "true"
	default:
		return s
	}
}

// resolveReceiveParams derives the ReceiveMessage tunables (batch size,
// long-poll wait, visibility timeout) from the consumer-side options only.
// Queue-level driver options are declare-time state and must not influence
// receive behavior — different consumers on the same queue are free to pick
// their own cadences.
func resolveReceiveParams(opts *queueapi.ConsumerOptions) (maxMessages, waitTime, visTimeout int32) {
	maxMessages = sqsMaxBatchSize
	waitTime = 20
	visTimeout = 0

	if opts == nil {
		return maxMessages, waitTime, visTimeout
	}

	if opts.Prefetch > 0 {
		n := int32(opts.Prefetch)
		if n > sqsMaxBatchSize {
			n = sqsMaxBatchSize
		}
		maxMessages = n
	}

	cb := opts.DriverBag("sqs")
	if v := cb.GetInt(optionWaitTime, 0); v > 0 {
		waitTime = int32(v)
	}
	if v := cb.GetInt(optionVisibilityTimeout, 0); v > 0 {
		visTimeout = int32(v)
	}
	return maxMessages, waitTime, visTimeout
}

func (d *Driver) Attach(ctx context.Context, queueID registry.ID, opts *queueapi.ConsumerOptions, deliveries chan<- *queueapi.Delivery) (context.CancelFunc, error) {
	d.mu.RLock()
	q, exists := d.queues[queueID]
	client := d.client
	var cfg *queueapi.Config
	var url string
	if exists {
		cfg = q.cfg
		url = q.url
	}
	d.mu.RUnlock()

	if !exists {
		return nil, queueapi.ErrQueueNotFound
	}
	if client == nil {
		return nil, queuesvc.ErrDriverNotStarted
	}

	consumerCtx, cancel := context.WithCancel(ctx)

	maxMessages, waitTime, visTimeout := resolveReceiveParams(opts)

	// Snapshot of the queue's codec for this consumer: redeclares on the
	// queue update future publishes and new attaches, but an in-flight
	// receive loop keeps the cfg it was started with.
	codec := queueCodec(cfg)

	receiveInput := &awssqs.ReceiveMessageInput{
		QueueUrl:              aws.String(url),
		MaxNumberOfMessages:   maxMessages,
		WaitTimeSeconds:       waitTime,
		MessageAttributeNames: []string{"All"},
		MessageSystemAttributeNames: []types.MessageSystemAttributeName{
			types.MessageSystemAttributeNameMessageGroupId,
			types.MessageSystemAttributeNameMessageDeduplicationId,
		},
	}
	if visTimeout > 0 {
		receiveInput.VisibilityTimeout = visTimeout
	}

	go func() {
		for {
			select {
			case <-consumerCtx.Done():
				return
			case <-d.lifecycleCtxDone():
				return
			default:
			}

			result, err := client.ReceiveMessage(consumerCtx, receiveInput)
			if err != nil {
				if consumerCtx.Err() != nil {
					return
				}
				d.logger.Error("sqs receive error",
					zap.String("queue", queueID.String()),
					zap.Error(err))
				if !waitWithContext(consumerCtx, d.lifecycleCtxDone(), time.Second) {
					return
				}
				continue
			}

			for _, sqsMsg := range result.Messages {
				if sqsMsg.Body == nil {
					continue
				}
				msg := queueapi.AcquireMessage(payload.NewPayload([]byte(*sqsMsg.Body), codec))
				if sqsMsg.MessageId != nil {
					msg.ID = *sqsMsg.MessageId
				}

				for k, v := range applyReceivedAttributes(sqsMsg) {
					msg.Headers.Set(k, v)
				}

				receiptHandle := aws.ToString(sqsMsg.ReceiptHandle)
				queueURL := url

				delivery := &queueapi.Delivery{
					Message: msg,
					Ack:     buildAck(client, queueURL, receiptHandle),
					Nack:    buildNack(client, queueURL, receiptHandle),
				}

				if !deliverOrRelease(consumerCtx, d.lifecycleCtxDone(), deliveries, delivery) {
					return
				}
			}
		}
	}()

	return cancel, nil
}

// buildQueueAttributes constructs SQS queue attributes from the driver config
// and per-queue options. Returns nil when no attributes need to be set.
func (d *Driver) buildQueueAttributes(name string, cfg *queueapi.Config) map[string]string {
	hasRetention := d.cfg.MessageRetentionPeriod > 0
	hasDelay := d.cfg.DefaultDelaySeconds > 0
	visTimeout := int32(0)
	if cfg != nil {
		visTimeout = int32(cfg.DriverBag("sqs").GetInt(optionQueueDefaultVisibilityTimeout, 0))
	}
	fifo := isFIFOName(name)

	if !hasRetention && !hasDelay && visTimeout <= 0 && !fifo {
		return nil
	}

	out := make(map[string]string)
	if hasRetention {
		out[string(types.QueueAttributeNameMessageRetentionPeriod)] = strconv.FormatInt(int64(d.cfg.MessageRetentionPeriod), 10)
	}
	if hasDelay {
		out[string(types.QueueAttributeNameDelaySeconds)] = strconv.FormatInt(int64(d.cfg.DefaultDelaySeconds), 10)
	}
	if visTimeout > 0 {
		out[string(types.QueueAttributeNameVisibilityTimeout)] = strconv.FormatInt(int64(visTimeout), 10)
	}
	if fifo {
		out[string(types.QueueAttributeNameFifoQueue)] = "true"
	}

	return out
}

func (d *Driver) DeclareQueue(ctx context.Context, queueID registry.ID, cfg *queueapi.Config) error {
	if err := queuesvc.ValidateCodec(d.tc, queueCodec(cfg)); err != nil {
		return err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Redeclare is an update, not a no-op: swap in the latest cfg pointer
	// so subsequent Publish / GetQueueInfo see the caller's latest options.
	// Skip re-resolving the queue URL — the SQS-side queue identity did not
	// change, only the local config did.
	if existing, exists := d.queues[queueID]; exists {
		existing.cfg = cfg
		return nil
	}

	if d.client == nil {
		return queuesvc.ErrDriverNotStarted
	}

	name := queueName(queueID, cfg)

	getResult, err := d.client.GetQueueUrl(ctx, &awssqs.GetQueueUrlInput{
		QueueName: aws.String(name),
	})
	if err == nil {
		d.queues[queueID] = &declaredQueue{
			url: aws.ToString(getResult.QueueUrl),
			cfg: cfg,
		}
		d.logger.Debug("queue found",
			zap.String("driver", d.id.String()),
			zap.String("queue", queueID.String()),
			zap.String("sqs_name", name))
		return nil
	}

	// Only proceed to create if the queue genuinely doesn't exist.
	// Other errors (network, auth, throttling) should be surfaced.
	var queueNotFound *types.QueueDoesNotExist
	if !errors.As(err, &queueNotFound) {
		return apierror.New(apierror.Unavailable, "sqs get queue url").WithCause(err).WithRetryable(apierror.True)
	}

	createInput := &awssqs.CreateQueueInput{
		QueueName: aws.String(name),
	}
	if queueAttrs := d.buildQueueAttributes(name, cfg); queueAttrs != nil {
		createInput.Attributes = queueAttrs
	}

	createResult, err := d.client.CreateQueue(ctx, createInput)
	if err != nil {
		return apierror.New(apierror.Unavailable, "sqs create queue").WithCause(err).WithRetryable(apierror.True)
	}

	d.queues[queueID] = &declaredQueue{
		url: aws.ToString(createResult.QueueUrl),
		cfg: cfg,
	}

	d.logger.Debug("queue declared",
		zap.String("driver", d.id.String()),
		zap.String("queue", queueID.String()),
		zap.String("sqs_name", name))

	return nil
}

func (d *Driver) GetQueueInfo(ctx context.Context, queueID registry.ID) (attrs.Attributes, error) {
	d.mu.RLock()
	q, exists := d.queues[queueID]
	client := d.client
	var url string
	if exists {
		url = q.url
	}
	d.mu.RUnlock()

	if !exists {
		return nil, queueapi.ErrQueueNotFound
	}
	if client == nil {
		return nil, queuesvc.ErrDriverNotStarted
	}

	result, err := client.GetQueueAttributes(ctx, &awssqs.GetQueueAttributesInput{
		QueueUrl: aws.String(url),
		AttributeNames: []types.QueueAttributeName{
			types.QueueAttributeNameApproximateNumberOfMessages,
			types.QueueAttributeNameApproximateNumberOfMessagesNotVisible,
		},
	})
	if err != nil {
		return nil, apierror.New(apierror.Unavailable, "sqs get queue attributes").WithCause(err).WithRetryable(apierror.True)
	}

	return buildInfoBag(result.Attributes), nil
}

// buildInfoBag converts SQS queue attribute strings into the canonical stats
// bag. Visible + in-flight folded into StatsMessageCount so the total reflects
// the true broker-side backlog; ready and in-flight are also exposed
// separately for observability.
func buildInfoBag(raw map[string]string) attrs.Bag {
	info := attrs.NewBag()
	ready := parseIntAttr(raw, string(types.QueueAttributeNameApproximateNumberOfMessages))
	inFlight := parseIntAttr(raw, string(types.QueueAttributeNameApproximateNumberOfMessagesNotVisible))
	info.Set(queueapi.StatsMessageCount, ready+inFlight)
	info.Set(queueapi.StatsReady, ready)
	info.Set(queueapi.StatsInFlight, inFlight)
	return info
}

func parseIntAttr(raw map[string]string, key string) int {
	v, ok := raw[key]
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
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
	d.mu.Lock()
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.statusChan = make(chan any, 1)
	d.mu.Unlock()

	d.logger.Info("sqs driver started",
		zap.String("id", d.id.String()),
		zap.String("region", d.awsCfg.Region))

	return d.statusChan, nil
}

func (d *Driver) Stop(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cancel != nil {
		d.cancel()
	}

	d.queues = make(map[registry.ID]*declaredQueue)

	if d.statusChan != nil {
		close(d.statusChan)
	}

	d.logger.Info("sqs driver stopped", zap.String("id", d.id.String()))
	return nil
}
