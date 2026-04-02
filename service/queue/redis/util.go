// SPDX-License-Identifier: MPL-2.0

package redis

import (
	"encoding/json"
	"strings"

	goredis "github.com/redis/go-redis/v9"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
)

// marshalBody converts a payload to JSON bytes.
func marshalBody(p payload.Payload) ([]byte, error) {
	if p == nil {
		return []byte("null"), nil
	}
	return json.Marshal(p)
}

// unmarshalBody converts bytes to a payload.
func unmarshalBody(data []byte) payload.Payload {
	if len(data) == 0 {
		return nil
	}
	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		return payload.NewString(string(data))
	}
	return payload.New(result)
}

// marshalHeaders converts headers bag to JSON bytes.
func marshalHeaders(headers attrs.Bag) ([]byte, error) {
	return json.Marshal(map[string]any(headers))
}

// unmarshalHeaders converts JSON bytes to a headers bag.
func unmarshalHeaders(data []byte) attrs.Bag {
	bag := attrs.NewBag()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return bag
	}
	for k, v := range m {
		bag.Set(k, v)
	}
	return bag
}

// parseRedisMessage converts a Redis stream message to a queue Message.
func parseRedisMessage(redisMsg goredis.XMessage) *queueapi.Message {
	msg := &queueapi.Message{
		Headers: attrs.NewBag(),
	}

	if id, ok := redisMsg.Values["id"].(string); ok {
		msg.ID = id
	}

	if body, ok := redisMsg.Values["body"].(string); ok {
		msg.Body = unmarshalBody([]byte(body))
	}

	if headers, ok := redisMsg.Values["headers"].(string); ok {
		msg.Headers = unmarshalHeaders([]byte(headers))
	}

	return msg
}

// isGroupExistsError checks if the error is a "BUSYGROUP" error (consumer group already exists).
func isGroupExistsError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "BUSYGROUP")
}
