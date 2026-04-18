// SPDX-License-Identifier: MPL-2.0

// Package sqs provides AWS SQS queue driver configuration.
package sqs

import (
	"fmt"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Kind identifies the AWS SQS queue driver.
const Kind registry.Kind = "queue.driver.sqs"

// Config defines the AWS SQS queue driver configuration.
//
// AWS credentials are resolved through the shared config.aws resource
// referenced by the AWSConfig field. The config.aws resource is managed
// by service/aws/config/manager.go and provides region, credentials,
// and other AWS SDK settings.
type Config struct {
	// AWSConfig is a resource ID referencing a config.aws resource
	// managed by service/aws/config/manager.go.
	// Resolved at runtime via resource.GetRegistry(ctx).Acquire().
	AWSConfig registry.ID `json:"config"`

	// Endpoint is a custom endpoint URL (e.g. for LocalStack, ElasticMQ).
	// Sets BaseEndpoint on the AWS config.
	Endpoint string `json:"endpoint,omitempty"`

	// Lifecycle configures the supervisor lifecycle for this driver.
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`

	// MessageRetentionPeriod is the queue-level message retention in seconds (60–1209600).
	// Applied as a queue attribute on CreateQueue.
	// 0 uses the AWS default (345600 = 4 days).
	MessageRetentionPeriod int32 `json:"message_retention_period,omitempty"`

	// DefaultDelaySeconds is the default delivery delay for messages (0–900).
	// Applied as a queue attribute on CreateQueue.
	// 0 means no delay.
	DefaultDelaySeconds int32 `json:"default_delay_seconds,omitempty"`

	// DisableMessageChecksumValidation disables SQS message checksum validation
	// for SendMessage, SendMessageBatch, and ReceiveMessage operations.
	DisableMessageChecksumValidation bool `json:"disable_message_checksum_validation,omitempty"`

	// UseFIPS enables FIPS-compliant endpoints.
	UseFIPS bool `json:"use_fips,omitempty"`

	// UseDualStack enables dual-stack (IPv4 + IPv6) endpoints.
	UseDualStack bool `json:"use_dual_stack,omitempty"`
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.AWSConfig.Name == "" {
		return fmt.Errorf("sqs: aws config is required")
	}
	if c.MessageRetentionPeriod != 0 && (c.MessageRetentionPeriod < 60 || c.MessageRetentionPeriod > 1209600) {
		return fmt.Errorf("sqs: message_retention_period must be 60–1209600, got %d", c.MessageRetentionPeriod)
	}
	if c.DefaultDelaySeconds < 0 || c.DefaultDelaySeconds > 900 {
		return fmt.Errorf("sqs: default_delay_seconds must be 0–900, got %d", c.DefaultDelaySeconds)
	}
	return nil
}

// InitDefaults initializes default values.
func (c *Config) InitDefaults() {
}
