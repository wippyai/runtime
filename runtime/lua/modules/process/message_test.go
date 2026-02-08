package process

import (
	"context"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
)

func TestNewMessage(t *testing.T) {
	p := pid.PID{}
	topic := "test-topic"
	payloads := payload.Payloads{
		payload.NewPayload("test", payload.String),
	}

	msg := NewMessage(p, topic, payloads)

	if msg == nil {
		t.Fatal("NewMessage returned nil")
	}
	if msg.Topic != topic {
		t.Errorf("expected topic %s, got %s", topic, msg.Topic)
	}
	if len(msg.Payloads) != 1 {
		t.Errorf("expected 1 payload, got %d", len(msg.Payloads))
	}
	if msg.From != p {
		t.Error("PID mismatch")
	}
}

func TestWrapMessage(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	msg := NewMessage(pid.PID{}, "topic", nil)
	wrapped := WrapMessage(l, msg)

	if wrapped.Type() != lua.LTUserData {
		t.Errorf("expected userdata, got %s", wrapped.Type())
	}

	ud := wrapped.(*lua.LUserData)
	if ud.Value == nil {
		t.Error("userdata value is nil")
	}
	if ud.Metatable == nil {
		t.Error("userdata metatable is nil")
	}
}

func TestMessageToString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bindProcess(l)
	msg := NewMessage(pid.PID{}, "test-topic", nil)
	wrapped := WrapMessage(l, msg)
	l.SetGlobal("msg", wrapped)

	err := l.DoString(`
		local str = tostring(msg)
		if not string.find(str, "process.Message") then
			error("expected string to contain 'process.Message', got: " .. str)
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestMessageTopic(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bindProcess(l)
	msg := NewMessage(pid.PID{}, "my-topic", nil)
	wrapped := WrapMessage(l, msg)
	l.SetGlobal("msg", wrapped)

	err := l.DoString(`
		local topic = msg:topic()
		if topic ~= "my-topic" then
			error("expected 'my-topic', got: " .. tostring(topic))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestMessageFrom(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bindProcess(l)
	p, _ := pid.ParsePID("{node1|process1|123}")
	msg := NewMessage(p, "topic", nil)
	wrapped := WrapMessage(l, msg)
	l.SetGlobal("msg", wrapped)

	err := l.DoString(`
		local from = msg:from()
		if from == nil then
			error("expected from to not be nil")
		end
		if type(from) ~= "string" then
			error("expected from to be string")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestMessageFrom_EmptyPID(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bindProcess(l)
	msg := NewMessage(pid.PID{}, "topic", nil)
	wrapped := WrapMessage(l, msg)
	l.SetGlobal("msg", wrapped)

	err := l.DoString(`
		local from = msg:from()
		if type(from) ~= "string" then
			error("expected from to be string")
		end
		if from ~= "" then
			error("expected empty string for unset from, got: " .. tostring(from))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestMessagePayload(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bindProcess(l)
	payloads := payload.Payloads{
		payload.NewPayload("test-data", payload.String),
	}
	msg := NewMessage(pid.PID{}, "topic", payloads)
	wrapped := WrapMessage(l, msg)
	l.SetGlobal("msg", wrapped)
	l.SetContext(context.Background())

	err := l.DoString(`
		local p = msg:payload()
		if p == nil then
			error("expected payload to not be nil")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestMessageHandler(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bindProcess(l)

	ctx := context.Background()
	p, _ := pid.ParsePID("{node1|process1|123}")
	topic := "test-topic"
	payloads := []payload.Payload{
		payload.NewPayload("data", payload.String),
	}

	result := MessageHandler(ctx, l, p, topic, payloads)

	if result.Type() != lua.LTUserData {
		t.Errorf("expected userdata, got %s", result.Type())
	}

	ud := result.(*lua.LUserData)
	msg, ok := ud.Value.(*Message)
	if !ok {
		t.Fatal("expected Message type")
	}
	if msg.Topic != topic {
		t.Errorf("expected topic %s, got %s", topic, msg.Topic)
	}
	if msg.From != p {
		t.Error("PID mismatch")
	}
	if len(msg.Payloads) != 1 {
		t.Errorf("expected 1 payload, got %d", len(msg.Payloads))
	}
}
