package transport

import (
	"context"
	"errors"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
)

type commonTestTranscoder struct {
	err     error
	payload payload.Payload
}

func (t *commonTestTranscoder) Transcode(payload.Payload, payload.Format) (payload.Payload, error) {
	if t.err != nil {
		return nil, t.err
	}
	return t.payload, nil
}

func (t *commonTestTranscoder) Unmarshal(payload.Payload, interface{}) error { return nil }

func TestPayloadsToArgs(t *testing.T) {
	args, err := PayloadsToArgs(context.Background(), payload.Payloads{
		nil,
		payload.New(7),
		payload.New("x"),
	})
	if err != nil {
		t.Fatalf("PayloadsToArgs() error = %v", err)
	}
	if len(args) != 2 || args[0] != 7 || args[1] != "x" {
		t.Fatalf("PayloadsToArgs() args = %#v, want [7 \"x\"]", args)
	}
}

func TestPayloadsToArgs_NoTranscoder(t *testing.T) {
	_, err := PayloadsToArgs(context.Background(), payload.Payloads{
		payload.NewPayload(`{"a":1}`, payload.JSON),
	})
	if err == nil {
		t.Fatal("PayloadsToArgs() expected transcoder not found error")
	}
}

func TestPayloadsToArgs_TranscodeError(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &commonTestTranscoder{err: errors.New("boom")})

	_, err := PayloadsToArgs(ctx, payload.Payloads{
		payload.NewPayload(`{"a":1}`, payload.JSON),
	})
	if err == nil {
		t.Fatal("PayloadsToArgs() expected transcode error")
	}
}

func TestPayloadsToArgs_TranscodeSuccess(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &commonTestTranscoder{
		payload: payload.New(map[string]any{"ok": true}),
	})

	args, err := PayloadsToArgs(ctx, payload.Payloads{
		payload.NewPayload(`{"ok":true}`, payload.JSON),
	})
	if err != nil {
		t.Fatalf("PayloadsToArgs() error = %v", err)
	}
	if len(args) != 1 {
		t.Fatalf("PayloadsToArgs() len = %d, want 1", len(args))
	}
	got, ok := args[0].(map[string]any)
	if !ok || got["ok"] != true {
		t.Fatalf("PayloadsToArgs() args[0] = %#v, want map[ok:true]", args[0])
	}
}

var _ payload.Transcoder = (*commonTestTranscoder)(nil)
