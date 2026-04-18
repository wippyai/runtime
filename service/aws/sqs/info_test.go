// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	queueapi "github.com/wippyai/runtime/api/queue"
)

// A queue with both visible and in-flight messages must report the sum as
// the total message_count, surface the visible slice as "ready", and expose
// the in-flight slice under StatsInFlight.
func TestBuildInfoBag_FoldsInFlight(t *testing.T) {
	raw := map[string]string{
		"ApproximateNumberOfMessages":           "5",
		"ApproximateNumberOfMessagesNotVisible": "3",
	}
	bag := buildInfoBag(raw)

	assert.Equal(t, 8, bag.GetInt(queueapi.StatsMessageCount, 0), "total = ready + in-flight")
	assert.Equal(t, 5, bag.GetInt(queueapi.StatsReady, 0))
	assert.Equal(t, 3, bag.GetInt(queueapi.StatsInFlight, 0))
}

// Missing attributes must not poison the stats — a freshly-created queue
// returns empty maps, and the driver should still produce a valid bag with
// zero counts.
func TestBuildInfoBag_MissingAttributes(t *testing.T) {
	bag := buildInfoBag(map[string]string{})
	assert.Equal(t, 0, bag.GetInt(queueapi.StatsMessageCount, 0))
	assert.Equal(t, 0, bag.GetInt(queueapi.StatsReady, 0))
	assert.Equal(t, 0, bag.GetInt(queueapi.StatsInFlight, 0))
}

func TestBuildInfoBag_OnlyVisible(t *testing.T) {
	raw := map[string]string{"ApproximateNumberOfMessages": "7"}
	bag := buildInfoBag(raw)
	assert.Equal(t, 7, bag.GetInt(queueapi.StatsMessageCount, 0))
	assert.Equal(t, 7, bag.GetInt(queueapi.StatsReady, 0))
	assert.Equal(t, 0, bag.GetInt(queueapi.StatsInFlight, 0))
}
