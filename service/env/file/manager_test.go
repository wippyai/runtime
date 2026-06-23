// SPDX-License-Identifier: MPL-2.0

package file

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	envsvc "github.com/wippyai/runtime/api/service/env"
	"go.uber.org/zap"
)

type mockBus struct {
	events []event.Event
}

func (m *mockBus) Send(_ context.Context, e event.Event) {
	m.events = append(m.events, e)
}

func (m *mockBus) Subscribe(context.Context, event.System, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockBus) SubscribeP(context.Context, event.System, event.Kind, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockBus) Unsubscribe(context.Context, event.SubscriberID) {}

type mockTranscoder struct {
	config     *envsvc.FileStorageConfig
	shouldFail bool
}

func (m *mockTranscoder) Unmarshal(_ payload.Payload, out any) error {
	if m.shouldFail {
		return assert.AnError
	}
	if cfg, ok := out.(*envsvc.FileStorageConfig); ok && m.config != nil {
		*cfg = *m.config
	}
	return nil
}

func (m *mockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

func TestManager_AddAutoCreatesFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, ".wippy", ".env")
	bus := &mockBus{}
	dtt := &mockTranscoder{
		config: &envsvc.FileStorageConfig{
			FilePath:   filePath,
			AutoCreate: true,
			FileMode:   0600,
			DirMode:    0700,
		},
	}
	mgr := NewManager(bus, dtt, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.ID{NS: "app.env", Name: "file"},
		Kind: envsvc.StorageFile,
		Data: payload.New(nil),
	}

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	info, err := os.Stat(filePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	dirInfo, err := os.Stat(filepath.Dir(filePath))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0700), dirInfo.Mode().Perm())

	require.Len(t, bus.events, 1)
	assert.Equal(t, env.StorageRegister, bus.events[0].Kind)

	storage, ok := mgr.GetStorage(entry.ID)
	require.True(t, ok)
	require.NotNil(t, storage)
}

func TestManager_AddDoesNotCreateFileWhenDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, ".wippy", ".env")
	bus := &mockBus{}
	dtt := &mockTranscoder{
		config: &envsvc.FileStorageConfig{
			FilePath:   filePath,
			AutoCreate: false,
			FileMode:   0600,
			DirMode:    0700,
		},
	}
	mgr := NewManager(bus, dtt, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.ID{NS: "app.env", Name: "file"},
		Kind: envsvc.StorageFile,
		Data: payload.New(nil),
	}

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	_, err = os.Stat(filePath)
	require.True(t, os.IsNotExist(err), "expected %s not to be created", filePath)
}
