// SPDX-License-Identifier: MPL-2.0

package i2p

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	netservice "github.com/wippyai/runtime/service/net"
)

// mockTranscoder populates the target via unmarshalFunc; used to exercise
// Driver.Create without wiring up the real payload transcoder graph.
type mockTranscoder struct {
	unmarshalFunc func(payload.Payload, any) error
}

func (m *mockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

func (m *mockTranscoder) Marshal(any) (payload.Payload, error) { return nil, errors.New("unused") }

func (m *mockTranscoder) Unmarshal(p payload.Payload, v any) error {
	if m.unmarshalFunc == nil {
		return errors.New("unmarshal not set")
	}
	return m.unmarshalFunc(p, v)
}

func makeI2PEntry() registry.Entry {
	return registry.Entry{
		ID:   registry.NewID("app.net", "i2p"),
		Kind: netapi.KindI2P,
		Data: payload.New(map[string]any{}),
	}
}

func TestDriver_Kind(t *testing.T) {
	assert.Equal(t, netapi.KindI2P, NewDriver().Kind())
}

func TestDriver_Create_Success(t *testing.T) {
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, v any) error {
			cfg := v.(*netapi.I2PConfig)
			cfg.Host = "127.0.0.1"
			cfg.Port = 7656
			cfg.SessionName = "test-session"
			return nil
		},
	}
	deps := netservice.Deps{Transcoder: dtt}

	svc, err := NewDriver().Create(context.Background(), makeI2PEntry(), deps)
	require.NoError(t, err)
	require.NotNil(t, svc)

	s, ok := svc.(*Service)
	require.True(t, ok, "driver must return *Service")
	assert.Equal(t, "127.0.0.1:7656", s.addr)
	assert.Equal(t, "test-session", s.sessionName)
}

func TestDriver_Create_DefaultSessionName(t *testing.T) {
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, v any) error {
			cfg := v.(*netapi.I2PConfig)
			cfg.Host = "127.0.0.1"
			cfg.Port = 7656
			return nil
		},
	}
	deps := netservice.Deps{Transcoder: dtt}

	svc, err := NewDriver().Create(context.Background(), makeI2PEntry(), deps)
	require.NoError(t, err)
	assert.Equal(t, "wippy", svc.(*Service).sessionName)
}

func TestDriver_Create_DecodeError(t *testing.T) {
	decodeErr := errors.New("bad config bytes")
	dtt := &mockTranscoder{
		unmarshalFunc: func(payload.Payload, any) error { return decodeErr },
	}
	deps := netservice.Deps{Transcoder: dtt}

	svc, err := NewDriver().Create(context.Background(), makeI2PEntry(), deps)
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "i2p")
	assert.ErrorIs(t, err, decodeErr)
}

func TestDriver_Create_ValidationError_MissingHost(t *testing.T) {
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, v any) error {
			cfg := v.(*netapi.I2PConfig)
			cfg.Port = 7656
			return nil
		},
	}
	deps := netservice.Deps{Transcoder: dtt}

	svc, err := NewDriver().Create(context.Background(), makeI2PEntry(), deps)
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.ErrorIs(t, err, netapi.ErrHostRequired)
}

func TestDriver_Create_NilData(t *testing.T) {
	dtt := &mockTranscoder{}
	deps := netservice.Deps{Transcoder: dtt}

	entry := registry.Entry{
		ID:   registry.NewID("app.net", "i2p"),
		Kind: netapi.KindI2P,
		Data: nil,
	}
	svc, err := NewDriver().Create(context.Background(), entry, deps)
	require.Error(t, err)
	assert.Nil(t, svc)
}
