// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/store"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"go.uber.org/zap"
)

type linearBoundaryEngine struct {
	*boundaryKVEngine
	normalGets  int
	normalScans int
	linearGets  int
	linearScans int
}

func (e *linearBoundaryEngine) Get(string) (kvapi.Entry, error) {
	e.normalGets++
	return kvapi.Entry{Key: "linear:app:stale", Value: []byte(`"stale"`), Version: 1}, nil
}
func (e *linearBoundaryEngine) Scan(string, func(kvapi.Entry) bool) error {
	e.normalScans++
	return nil
}
func (e *linearBoundaryEngine) GetLinearizable(key string) (kvapi.Entry, error) {
	e.linearGets++
	return kvapi.Entry{Key: key, Value: []byte(`"fresh-entry"`), Version: 7}, nil
}
func (e *linearBoundaryEngine) ScanAtIndex(prefix string, fn func(kvapi.Entry) bool) (uint64, error) {
	e.linearScans++
	fn(kvapi.Entry{Key: prefix + "item", Value: []byte(`"fresh-list"`), Version: 8})
	return 42, nil
}

func TestD11KVLinearizableRouting(t *testing.T) {
	engine := &linearBoundaryEngine{boundaryKVEngine: &boundaryKVEngine{}}
	s := NewStoreWithInfo(
		registry.ParseID("test:store"), "linear", engine, boundaryKVTranscoder{}, zap.NewNop(),
		store.Info{Consistency: store.ConsistencyLinearizable, List: true, Versioned: true},
	)

	entry, err := s.Entry(context.Background(), registry.ParseID("app:item"))
	require.NoError(t, err)
	require.Equal(t, store.Version(7), entry.Version)
	require.Equal(t, []byte(`"fresh-entry"`), entry.Value.Data())

	page, err := s.List(context.Background(), store.ListOptions{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, registry.ParseID("item"), page.Items[0].Key)
	require.Equal(t, store.Version(8), page.Items[0].Version)
	require.Equal(t, []byte(`"fresh-list"`), page.Items[0].Value.Data())

	require.Equal(t, 1, engine.linearGets)
	require.Equal(t, 1, engine.linearScans)
	require.Zero(t, engine.normalGets)
	require.Zero(t, engine.normalScans)
}

var _ payload.Transcoder = boundaryKVTranscoder{}
