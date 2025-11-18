// Package stats provides process and host statistics collection.
package stats

import "github.com/wippyai/runtime/api/relay"

const (
	// SystemEndpoint is the reserved UniqID for host system control
	SystemEndpoint = "@system"

	// System control topics
	TopicStatsEnable  relay.Topic = "@system/stats-enable"
	TopicStatsDisable relay.Topic = "@system/stats-disable"
	TopicStatsCollect relay.Topic = "@system/stats-collect"
)
