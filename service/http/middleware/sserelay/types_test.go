// SPDX-License-Identifier: MPL-2.0

package sserelay

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayCommandJSON(t *testing.T) {
	cmd := RelayCommand{
		TargetPID:         "{n1@app:proc|123}",
		MessageTopic:      "llm.delta",
		HeartbeatInterval: "5s",
		IdleTimeout:       "30s",
		HardTimeout:       "2m",
		Metadata: map[string]any{
			"user_id": "u1",
		},
	}

	data, err := json.Marshal(cmd)
	require.NoError(t, err)

	var decoded RelayCommand
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, cmd.TargetPID, decoded.TargetPID)
	assert.Equal(t, cmd.MessageTopic, decoded.MessageTopic)
	assert.Equal(t, cmd.HeartbeatInterval, decoded.HeartbeatInterval)
	assert.Equal(t, cmd.IdleTimeout, decoded.IdleTimeout)
	assert.Equal(t, cmd.HardTimeout, decoded.HardTimeout)
	assert.Equal(t, "u1", decoded.Metadata["user_id"])
}

func TestConstants(t *testing.T) {
	assert.Equal(t, "sse_relay", MiddlewareName)
	assert.Equal(t, "X-SSE-Relay", RelayHeader)
	assert.Equal(t, 30*time.Second, DefaultHeartbeatInterval)
	assert.Equal(t, 32, DefaultChannelCapacity)

	assert.Equal(t, "sse.message", MessageTopic)
	assert.Equal(t, "sse.join", JoinTopic)
	assert.Equal(t, "sse.leave", LeaveTopic)
	assert.Equal(t, "sse.control", ControlTopic)
	assert.Equal(t, "sse.close", CloseTopic)
	assert.Equal(t, "sse.heartbeat", HeartbeatTopic)
}
