// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/google/uuid"
	"github.com/wippyai/runtime/api/attrs"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	queuesvc "github.com/wippyai/runtime/service/queue"
	"go.uber.org/zap"
)

type declaredQueue struct {
	opts attrs.Attributes
	url  string
}

// Driver implements the AWS SQS queue driver.
type Driver struct {
	ctx        context.Context
	logger     *zap.Logger
	client     *awssqs.Client
	queues     map[registry.ID]*declaredQueue
	cancel     context.CancelFunc
	statusChan chan any
	id         registry.ID
	awsCfg     aws.Config
	mu         sync.RWMutex
}

// NewDriver creates a new SQS driver instance.
func NewDriver(id registry.ID, awsCfg aws.Config, logger *zap.Logger) *Driver {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Driver{
		id:     id,
		awsCfg: awsCfg,
		logger: logger,
		queues: make(map[registry.ID]*declaredQueue),
	}
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

		msgAttrs := make(map[string]types.MessageAttributeValue)
		if msg.Headers != nil {
			for k, v := range msg.Headers {
				msgAttrs[k] = types.MessageAttributeValue{
					DataType:    aws.String("String"),
					StringValue: aws.String(fmt.Sprintf("%v", v)),
				}
			}
		}

		body, err := marshalBody(msg.Body)
		if err != nil {
			return fmt.Errorf("sqs marshal body: %w", err)
		}

		input := &awssqs.SendMessageInput{
			QueueUrl:               aws.String(q.url),
			MessageBody:            aws.String(string(body)),
			MessageAttributes:      msgAttrs,
			MessageDeduplicationId: nil,
		}

		if _, err := client.SendMessage(ctx, input); err != nil {
			return fmt.Errorf("sqs send message: %w", err)
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

	consumerCtx, cancel := context.WithCancel(ctx)

	go func() {
		for {
			select {
			case <-consumerCtx.Done():
				return
			case <-d.lifecycleCtxDone():
				return
			default:
			}

			result, err := client.ReceiveMessage(consumerCtx, &awssqs.ReceiveMessageInput{
				QueueUrl:              aws.String(q.url),
				MaxNumberOfMessages:   10,
				WaitTimeSeconds:       20,
				MessageAttributeNames: []string{"All"},
			})
			if err != nil {
				if consumerCtx.Err() != nil {
					return
				}
				d.logger.Error("sqs receive error",
					zap.String("queue", queueID.String()),
					zap.Error(err))
				time.Sleep(time.Second)
				continue
			}

			for _, sqsMsg := range result.Messages {
				msg := &queueapi.Message{
					ID:      aws.ToString(sqsMsg.MessageId),
					Body:    unmarshalBody([]byte(aws.ToString(sqsMsg.Body))),
					Headers: attrs.NewBag(),
				}

				for k, v := range sqsMsg.MessageAttributes {
					msg.Headers.Set(k, aws.ToString(v.StringValue))
				}

				receiptHandle := aws.ToString(sqsMsg.ReceiptHandle)
				queueURL := q.url

				delivery := &queueapi.Delivery{
					Message: msg,
					Ack: func(_ context.Context) error {
						_, err := client.DeleteMessage(context.Background(), &awssqs.DeleteMessageInput{
							QueueUrl:      aws.String(queueURL),
							ReceiptHandle: aws.String(receiptHandle),
						})
						return err
					},
					Nack: func(_ context.Context) error {
						_, err := client.ChangeMessageVisibility(context.Background(), &awssqs.ChangeMessageVisibilityInput{
							QueueUrl:          aws.String(queueURL),
							ReceiptHandle:     aws.String(receiptHandle),
							VisibilityTimeout: 0,
						})
						return err
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

func (d *Driver) DeclareQueue(ctx context.Context, queueID registry.ID, opts attrs.Attributes) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.queues[queueID]; exists {
		return nil
	}

	if d.client == nil {
		return queuesvc.ErrDriverNotStarted
	}

	name := d.queueName(queueID, opts)

	// Try to get existing queue URL first
	getResult, err := d.client.GetQueueUrl(ctx, &awssqs.GetQueueUrlInput{
		QueueName: aws.String(name),
	})
	if err == nil {
		d.queues[queueID] = &declaredQueue{
			url:  aws.ToString(getResult.QueueUrl),
			opts: opts,
		}
		d.logger.Debug("queue found",
			zap.String("driver", d.id.String()),
			zap.String("queue", queueID.String()),
			zap.String("sqs_name", name))
		return nil
	}

	// Queue doesn't exist, create it
	createResult, err := d.client.CreateQueue(ctx, &awssqs.CreateQueueInput{
		QueueName: aws.String(name),
	})
	if err != nil {
		return fmt.Errorf("sqs create queue: %w", err)
	}

	d.queues[queueID] = &declaredQueue{
		url:  aws.ToString(createResult.QueueUrl),
		opts: opts,
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
	d.mu.RUnlock()

	if !exists {
		return nil, queueapi.ErrQueueNotFound
	}
	if client == nil {
		return nil, queuesvc.ErrDriverNotStarted
	}

	result, err := client.GetQueueAttributes(ctx, &awssqs.GetQueueAttributesInput{
		QueueUrl: aws.String(q.url),
		AttributeNames: []types.QueueAttributeName{
			types.QueueAttributeNameApproximateNumberOfMessages,
			types.QueueAttributeNameApproximateNumberOfMessagesNotVisible,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("sqs get queue attributes: %w", err)
	}

	info := attrs.NewBag()
	if v, ok := result.Attributes[string(types.QueueAttributeNameApproximateNumberOfMessages)]; ok {
		info.Set(queueapi.StatsMessageCount, v)
		info.Set(queueapi.StatsReady, v)
	}

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
	d.mu.Lock()
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.client = awssqs.NewFromConfig(d.awsCfg)
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

	d.client = nil
	d.queues = make(map[registry.ID]*declaredQueue)

	if d.statusChan != nil {
		close(d.statusChan)
	}

	d.logger.Info("sqs driver stopped", zap.String("id", d.id.String()))
	return nil
}
