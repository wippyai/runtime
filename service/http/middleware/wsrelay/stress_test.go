package wsrelay

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStress_RelayCommandSerialization(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	var wg sync.WaitGroup
	numGoroutines := 100
	iterations := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				cmd := RelayCommand{
					TargetPID:         "test:target:123",
					MessageTopic:      "custom.topic",
					HeartbeatInterval: "15s",
					Metadata: map[string]any{
						"user_id": "user123",
						"room":    "lobby",
						"count":   j,
					},
				}

				data, err := json.Marshal(cmd)
				require.NoError(t, err)

				var parsed RelayCommand
				err = json.Unmarshal(data, &parsed)
				require.NoError(t, err)

				assert.Equal(t, cmd.TargetPID, parsed.TargetPID)
			}
		}(i)
	}

	wg.Wait()
}

func TestStress_JoinInfoSerialization(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	var wg sync.WaitGroup
	numGoroutines := 100
	iterations := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				info := JoinInfo{
					ClientPID: "test:client:456",
					Metadata: map[string]any{
						"username":  "testuser",
						"goroutine": id,
						"iteration": j,
					},
				}

				data, err := json.Marshal(info)
				require.NoError(t, err)

				var parsed JoinInfo
				err = json.Unmarshal(data, &parsed)
				require.NoError(t, err)

				assert.Equal(t, info.ClientPID, parsed.ClientPID)
			}
		}(i)
	}

	wg.Wait()
}

func TestStress_HeartbeatInfoSerialization(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	var wg sync.WaitGroup
	numGoroutines := 100
	iterations := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				info := HeartbeatInfo{
					ClientPID:    "test:client:456",
					Uptime:       "5m30s",
					MessageCount: int64(j),
					Metadata: map[string]any{
						"status":    "active",
						"goroutine": id,
					},
				}

				data, err := json.Marshal(info)
				require.NoError(t, err)

				var parsed HeartbeatInfo
				err = json.Unmarshal(data, &parsed)
				require.NoError(t, err)

				assert.Equal(t, info.ClientPID, parsed.ClientPID)
				assert.Equal(t, info.MessageCount, parsed.MessageCount)
			}
		}(i)
	}

	wg.Wait()
}

func TestSecurity_MaliciousJSON(t *testing.T) {
	testCases := []struct {
		name string
		json string
	}{
		{"deeply nested", `{"a":{"b":{"c":{"d":{"e":{"f":"deep"}}}}}}`},
		{"large array", `{"arr":[` + repeatString(`1,`, 1000) + `1]}`},
		{"unicode escape", `{"data":"\u0000\u0001\u0002"}`},
		{"null bytes", `{"data":"test\u0000value"}`},
		{"empty object", `{}`},
		{"empty string fields", `{"target_pid":"","message_topic":"","heartbeat_interval":""}`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var cmd RelayCommand
			err := json.Unmarshal([]byte(tc.json), &cmd)
			if err == nil {
				_, marshalErr := json.Marshal(cmd)
				assert.NoError(t, marshalErr, "should be able to re-marshal")
			}
		})
	}
}

func TestSecurity_MetadataInjection(t *testing.T) {
	testCases := []struct {
		metadata map[string]any
		name     string
	}{
		{map[string]any{"key": nil}, "null value"},
		{map[string]any{"nested": map[string]any{"deep": "value"}}, "nested object"},
		{map[string]any{"arr": []int{1, 2, 3}}, "array value"},
		{map[string]any{"key": "<script>alert('xss')</script>"}, "special chars"},
		{map[string]any{"key": "'; DROP TABLE users; --"}, "sql injection"},
		{map[string]any{"key": "../../../etc/passwd"}, "path traversal"},
		{map[string]any{"key": repeatString("a", 100000)}, "large string"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := RelayCommand{
				TargetPID: "test:target:123",
				Metadata:  tc.metadata,
			}

			data, err := json.Marshal(cmd)
			require.NoError(t, err)

			var parsed RelayCommand
			err = json.Unmarshal(data, &parsed)
			require.NoError(t, err)
		})
	}
}

func TestEdge_EmptyFields(t *testing.T) {
	t.Run("empty relay command", func(t *testing.T) {
		cmd := RelayCommand{}
		data, err := json.Marshal(cmd)
		require.NoError(t, err)

		var parsed RelayCommand
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)

		assert.Empty(t, parsed.TargetPID)
		assert.Empty(t, parsed.MessageTopic)
		assert.Nil(t, parsed.Metadata)
	})

	t.Run("empty join info", func(t *testing.T) {
		info := JoinInfo{}
		data, err := json.Marshal(info)
		require.NoError(t, err)

		var parsed JoinInfo
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)

		assert.Empty(t, parsed.ClientPID)
		assert.Nil(t, parsed.Metadata)
	})

	t.Run("empty heartbeat info", func(t *testing.T) {
		info := HeartbeatInfo{}
		data, err := json.Marshal(info)
		require.NoError(t, err)

		var parsed HeartbeatInfo
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)

		assert.Empty(t, parsed.ClientPID)
		assert.Zero(t, parsed.MessageCount)
	})
}

func TestEdge_LargeMetadata(t *testing.T) {
	largeMetadata := make(map[string]any)
	for i := 0; i < 1000; i++ {
		largeMetadata[fmt.Sprintf("key%d", i)] = repeatString("value", 100)
	}

	cmd := RelayCommand{
		TargetPID: "test:target:123",
		Metadata:  largeMetadata,
	}

	data, err := json.Marshal(cmd)
	require.NoError(t, err)

	var parsed RelayCommand
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Len(t, parsed.Metadata, 1000)
}

func TestEdge_SpecialPIDFormats(t *testing.T) {
	testPIDs := []string{
		"simple",
		"namespace:name:id",
		"a:b:c:d:e:f",
		"unicode:키:123",
		"spaces: test : 123",
		"",
		string(make([]byte, 10000)),
	}

	for _, pid := range testPIDs {
		t.Run("pid="+truncateString(pid, 20), func(t *testing.T) {
			cmd := RelayCommand{
				TargetPID: pid,
			}

			data, err := json.Marshal(cmd)
			require.NoError(t, err)

			var parsed RelayCommand
			err = json.Unmarshal(data, &parsed)
			require.NoError(t, err)

			assert.Equal(t, pid, parsed.TargetPID)
		})
	}
}

func TestEdge_TopicFormats(t *testing.T) {
	testTopics := []string{
		"simple",
		"namespace.topic.subtopic",
		"a.b.c.d.e.f",
		"",
		string(make([]byte, 10000)),
	}

	for _, topic := range testTopics {
		t.Run("topic="+truncateString(topic, 20), func(t *testing.T) {
			cmd := RelayCommand{
				TargetPID:    "test:target:123",
				MessageTopic: topic,
			}

			data, err := json.Marshal(cmd)
			require.NoError(t, err)

			var parsed RelayCommand
			err = json.Unmarshal(data, &parsed)
			require.NoError(t, err)

			assert.Equal(t, topic, parsed.MessageTopic)
		})
	}
}

func TestEdge_HeartbeatIntervalFormats(t *testing.T) {
	testIntervals := []string{
		"",
		"0s",
		"1s",
		"1m",
		"1h",
		"24h",
		"invalid",
		"-1s",
		"9999999h",
	}

	for _, interval := range testIntervals {
		t.Run("interval="+interval, func(t *testing.T) {
			cmd := RelayCommand{
				TargetPID:         "test:target:123",
				HeartbeatInterval: interval,
			}

			data, err := json.Marshal(cmd)
			require.NoError(t, err)

			var parsed RelayCommand
			err = json.Unmarshal(data, &parsed)
			require.NoError(t, err)

			assert.Equal(t, interval, parsed.HeartbeatInterval)
		})
	}
}

func BenchmarkRelayCommandMarshal(b *testing.B) {
	cmd := RelayCommand{
		TargetPID:         "test:target:123",
		MessageTopic:      "custom.topic",
		HeartbeatInterval: "15s",
		Metadata: map[string]any{
			"user_id": "user123",
			"room":    "lobby",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(cmd)
	}
}

func BenchmarkRelayCommandUnmarshal(b *testing.B) {
	data := []byte(`{"target_pid":"test:target:123","message_topic":"custom.topic","heartbeat_interval":"15s","metadata":{"user_id":"user123","room":"lobby"}}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var cmd RelayCommand
		_ = json.Unmarshal(data, &cmd)
	}
}

func BenchmarkJoinInfoMarshal(b *testing.B) {
	info := JoinInfo{
		ClientPID: "test:client:456",
		Metadata: map[string]any{
			"username": "testuser",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(info)
	}
}

func BenchmarkHeartbeatInfoMarshal(b *testing.B) {
	info := HeartbeatInfo{
		ClientPID:    "test:client:456",
		Uptime:       "5m30s",
		MessageCount: 42,
		Metadata: map[string]any{
			"status": "active",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(info)
	}
}

func repeatString(s string, count int) string {
	result := make([]byte, len(s)*count)
	for i := 0; i < count; i++ {
		copy(result[i*len(s):], s)
	}
	return string(result)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
