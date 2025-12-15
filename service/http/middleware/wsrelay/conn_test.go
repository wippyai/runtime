package wsrelay

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayCommand(t *testing.T) {
	t.Run("json serialization", func(t *testing.T) {
		cmd := RelayCommand{
			TargetPID:         "test:target:123",
			MessageTopic:      "custom.topic",
			HeartbeatInterval: "15s",
			Metadata: map[string]any{
				"user_id": "user123",
				"room":    "lobby",
			},
		}

		data, err := json.Marshal(cmd)
		require.NoError(t, err)

		var parsed RelayCommand
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)

		assert.Equal(t, cmd.TargetPID, parsed.TargetPID)
		assert.Equal(t, cmd.MessageTopic, parsed.MessageTopic)
		assert.Equal(t, cmd.HeartbeatInterval, parsed.HeartbeatInterval)
		assert.Equal(t, cmd.Metadata["user_id"], parsed.Metadata["user_id"])
	})

	t.Run("json with empty fields", func(t *testing.T) {
		cmd := RelayCommand{
			TargetPID: "test:target:123",
		}

		data, err := json.Marshal(cmd)
		require.NoError(t, err)

		var parsed RelayCommand
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)

		assert.Equal(t, cmd.TargetPID, parsed.TargetPID)
		assert.Empty(t, parsed.MessageTopic)
		assert.Empty(t, parsed.HeartbeatInterval)
		assert.Nil(t, parsed.Metadata)
	})

	t.Run("omitempty works correctly", func(t *testing.T) {
		cmd := RelayCommand{
			TargetPID: "test:target:123",
		}

		data, err := json.Marshal(cmd)
		require.NoError(t, err)

		assert.NotContains(t, string(data), "message_topic")
		assert.NotContains(t, string(data), "heartbeat_interval")
		assert.NotContains(t, string(data), "metadata")
	})
}

func TestJoinInfo(t *testing.T) {
	t.Run("json serialization", func(t *testing.T) {
		info := JoinInfo{
			ClientPID: "test:client:456",
			Metadata: map[string]any{
				"username": "testuser",
			},
		}

		data, err := json.Marshal(info)
		require.NoError(t, err)

		var parsed JoinInfo
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)

		assert.Equal(t, info.ClientPID, parsed.ClientPID)
		assert.Equal(t, info.Metadata["username"], parsed.Metadata["username"])
	})

	t.Run("empty metadata", func(t *testing.T) {
		info := JoinInfo{
			ClientPID: "test:client:456",
		}

		data, err := json.Marshal(info)
		require.NoError(t, err)

		var parsed JoinInfo
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)

		assert.Equal(t, info.ClientPID, parsed.ClientPID)
		assert.Nil(t, parsed.Metadata)
	})
}

func TestHeartbeatInfo(t *testing.T) {
	t.Run("json serialization", func(t *testing.T) {
		info := HeartbeatInfo{
			ClientPID:    "test:client:456",
			Uptime:       "5m30s",
			MessageCount: 42,
			Metadata: map[string]any{
				"status": "active",
			},
		}

		data, err := json.Marshal(info)
		require.NoError(t, err)

		var parsed HeartbeatInfo
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)

		assert.Equal(t, info.ClientPID, parsed.ClientPID)
		assert.Equal(t, info.Uptime, parsed.Uptime)
		assert.Equal(t, info.MessageCount, parsed.MessageCount)
		assert.Equal(t, info.Metadata["status"], parsed.Metadata["status"])
	})

	t.Run("zero values", func(t *testing.T) {
		info := HeartbeatInfo{
			ClientPID: "test:client:456",
		}

		data, err := json.Marshal(info)
		require.NoError(t, err)

		var parsed HeartbeatInfo
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)

		assert.Equal(t, info.ClientPID, parsed.ClientPID)
		assert.Empty(t, parsed.Uptime)
		assert.Zero(t, parsed.MessageCount)
	})
}

func TestTopicConstants(t *testing.T) {
	t.Run("all topics have ws prefix", func(t *testing.T) {
		topics := []string{
			MessageTopic,
			JoinTopic,
			LeaveTopic,
			ControlTopic,
			CloseTopic,
			HeartbeatTopic,
		}

		for _, topic := range topics {
			assert.True(t, len(topic) > 3, "topic should have content: %s", topic)
			assert.Contains(t, topic, "ws.", "topic should have ws prefix: %s", topic)
		}
	})

	t.Run("topics are unique", func(t *testing.T) {
		topics := map[string]bool{
			MessageTopic:   true,
			JoinTopic:      true,
			LeaveTopic:     true,
			ControlTopic:   true,
			CloseTopic:     true,
			HeartbeatTopic: true,
		}

		assert.Len(t, topics, 6, "all topics should be unique")
	})

	t.Run("topic values", func(t *testing.T) {
		assert.Equal(t, "ws.message", MessageTopic)
		assert.Equal(t, "ws.join", JoinTopic)
		assert.Equal(t, "ws.leave", LeaveTopic)
		assert.Equal(t, "ws.control", ControlTopic)
		assert.Equal(t, "ws.close", CloseTopic)
		assert.Equal(t, "ws.heartbeat", HeartbeatTopic)
	})
}

func TestDefaultHeartbeatInterval(t *testing.T) {
	assert.Equal(t, 30*time.Second, DefaultHeartbeatInterval)
}

func TestWSRelayHeader(t *testing.T) {
	assert.Equal(t, "X-WS-Relay", RelayHeader)
}
