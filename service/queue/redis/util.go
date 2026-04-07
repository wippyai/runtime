// SPDX-License-Identifier: MPL-2.0

package redis

import (
	"encoding/json"
	"fmt"
	"strings"

	goredis "github.com/redis/go-redis/v9"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
)

// queueCodec returns the codec format for a queue, defaulting to JSON.
func queueCodec(opts attrs.Attributes) string {
	if opts != nil {
		if codec := opts.GetString(queueapi.OptionCodec, ""); codec != "" {
			return codec
		}
	}
	return payload.JSON
}

// marshalBody converts a payload to bytes using the transcoder with the given codec format.
func marshalBody(tc payload.Transcoder, codec string, p payload.Payload) ([]byte, error) {
	if p == nil {
		return []byte("null"), nil
	}

	// Ensure we start from a golang/any payload
	src := p
	if src.Format() != payload.Golang {
		var err error
		src, err = tc.Transcode(src, payload.Golang)
		if err != nil {
			return nil, fmt.Errorf("transcode to golang: %w", err)
		}
	}

	// Transcode to the target codec format
	encoded, err := tc.Transcode(src, codec)
	if err != nil {
		return nil, fmt.Errorf("transcode to %s: %w", codec, err)
	}

	switch v := encoded.Data().(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("transcoded payload data is not []byte or string, got %T", v)
	}
}

// unmarshalBody converts message body bytes to a payload using the transcoder with the given codec format.
func unmarshalBody(tc payload.Transcoder, codec string, data []byte) payload.Payload {
	if len(data) == 0 {
		return nil
	}

	// Create a payload in the source codec format
	src := payload.NewPayload(data, codec)

	// Transcode to golang/any
	result, err := tc.Transcode(src, payload.Golang)
	if err != nil {
		// Fall back to raw string if transcoding fails
		return payload.NewString(string(data))
	}

	return payload.New(result.Data())
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
func parseRedisMessage(tc payload.Transcoder, codec string, redisMsg goredis.XMessage) *queueapi.Message {
	msg := &queueapi.Message{
		Headers: attrs.NewBag(),
	}

	if id, ok := redisMsg.Values["id"].(string); ok {
		msg.ID = id
	}

	if body, ok := redisMsg.Values["body"].(string); ok {
		msg.Body = unmarshalBody(tc, codec, []byte(body))
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
