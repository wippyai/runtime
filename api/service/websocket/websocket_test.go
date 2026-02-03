package websocket

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/pid"
)

func TestCommandIDs(t *testing.T) {
	assert.Equal(t, dispatcher.CommandID(80), Connect)
	assert.Equal(t, dispatcher.CommandID(81), Send)
	assert.Equal(t, dispatcher.CommandID(82), Receive)
	assert.Equal(t, dispatcher.CommandID(83), Close)
	assert.Equal(t, dispatcher.CommandID(84), Ping)
	assert.Equal(t, dispatcher.CommandID(85), Subscribe)
}

func TestMessageTypes(t *testing.T) {
	assert.Equal(t, 1, MessageText)
	assert.Equal(t, 2, MessageBinary)
}

func TestCompressionModes(t *testing.T) {
	assert.Equal(t, 0, CompressionDisabled)
	assert.Equal(t, 1, CompressionContextTakeover)
	assert.Equal(t, 2, CompressionNoContext)
}

func TestConnectCmd(t *testing.T) {
	cmd := ConnectCmd{
		URL:                  "wss://example.com/ws",
		Headers:              map[string]string{"Authorization": "Bearer token"},
		Protocols:            []string{"graphql-ws"},
		DialTimeout:          5 * time.Second,
		ReadTimeout:          30 * time.Second,
		WriteTimeout:         10 * time.Second,
		CompressionMode:      CompressionContextTakeover,
		CompressionThreshold: 1024,
		ReadLimit:            1024 * 1024,
		ChannelCapacity:      100,
	}

	assert.Equal(t, Connect, cmd.CmdID())
	assert.Equal(t, "wss://example.com/ws", cmd.URL)
	assert.Equal(t, "Bearer token", cmd.Headers["Authorization"])
	assert.Equal(t, []string{"graphql-ws"}, cmd.Protocols)
}

func TestSendCmd(t *testing.T) {
	cmd := SendCmd{
		ConnID:      123,
		Data:        []byte("hello"),
		MessageType: MessageText,
	}

	assert.Equal(t, Send, cmd.CmdID())
	assert.Equal(t, uint64(123), cmd.ConnID)
	assert.Equal(t, []byte("hello"), cmd.Data)
	assert.Equal(t, MessageText, cmd.MessageType)
}

func TestReceiveCmd(t *testing.T) {
	cmd := ReceiveCmd{ConnID: 456}

	assert.Equal(t, Receive, cmd.CmdID())
	assert.Equal(t, uint64(456), cmd.ConnID)
}

func TestCloseCmd(t *testing.T) {
	cmd := CloseCmd{
		ConnID: 789,
		Code:   1000,
		Reason: "normal closure",
	}

	assert.Equal(t, Close, cmd.CmdID())
	assert.Equal(t, uint64(789), cmd.ConnID)
	assert.Equal(t, 1000, cmd.Code)
	assert.Equal(t, "normal closure", cmd.Reason)
}

func TestPingCmd(t *testing.T) {
	cmd := PingCmd{
		ConnID: 111,
		Data:   []byte("ping"),
	}

	assert.Equal(t, Ping, cmd.CmdID())
	assert.Equal(t, uint64(111), cmd.ConnID)
	assert.Equal(t, []byte("ping"), cmd.Data)
}

func TestMessage(t *testing.T) {
	msg := Message{
		Data:        []byte("test message"),
		MessageType: MessageBinary,
		EOF:         false,
	}

	assert.Equal(t, []byte("test message"), msg.Data)
	assert.Equal(t, MessageBinary, msg.MessageType)
	assert.False(t, msg.EOF)
}

func TestMessage_EOF(t *testing.T) {
	msg := Message{
		EOF: true,
	}

	assert.True(t, msg.EOF)
}

func TestSubscribeCmd(t *testing.T) {
	cmd := SubscribeCmd{
		ConnID: 222,
		Topic:  "ws@222",
		PID:    pid.PID{Host: "test-host", UniqID: "proc-1"},
	}

	assert.Equal(t, Subscribe, cmd.CmdID())
	assert.Equal(t, uint64(222), cmd.ConnID)
	assert.Equal(t, "ws@222", cmd.Topic)
	assert.Equal(t, "test-host", cmd.PID.Host)
	assert.Equal(t, "proc-1", cmd.PID.UniqID)
}

func TestSubscription(t *testing.T) {
	sub := Subscription{
		ConnID: 333,
		Topic:  "ws@333",
	}

	assert.Equal(t, uint64(333), sub.ConnID)
	assert.Equal(t, "ws@333", sub.Topic)
}
