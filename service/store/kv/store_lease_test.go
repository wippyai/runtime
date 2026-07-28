// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/store"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"go.uber.org/zap"
)

type boundaryLease struct{ revokes atomic.Int32 }

func (*boundaryLease) ID() kvapi.LeaseID               { return "lease-1" }
func (*boundaryLease) TTL() time.Duration              { return time.Minute }
func (*boundaryLease) KeepAlive(context.Context) error { return nil }
func (l *boundaryLease) Revoke(context.Context) error  { l.revokes.Add(1); return nil }
func (*boundaryLease) Done() <-chan struct{}           { return make(chan struct{}) }

type boundaryKVEngine struct {
	lease           *boundaryLease
	setWithLeaseErr error
	absentOK        bool
	absentErr       error
}

func (*boundaryKVEngine) Get(string) (kvapi.Entry, error)           { return kvapi.Entry{}, kvapi.ErrKeyNotFound }
func (*boundaryKVEngine) Set(string, []byte) (kvapi.Version, error) { return 1, nil }
func (*boundaryKVEngine) Delete(string) error                       { return nil }
func (*boundaryKVEngine) SetIfAbsent(string, []byte) (kvapi.Version, bool, error) {
	return 1, true, nil
}
func (*boundaryKVEngine) CompareAndSwap(string, kvapi.Version, []byte) (kvapi.Version, bool, error) {
	return 1, true, nil
}
func (*boundaryKVEngine) CompareAndDelete(string, kvapi.Version) (bool, error) { return true, nil }
func (*boundaryKVEngine) Txn([]kvapi.TxnOp) (bool, error)                      { return true, nil }
func (*boundaryKVEngine) Scan(string, func(kvapi.Entry) bool) error            { return nil }
func (*boundaryKVEngine) Watch(context.Context, string) (kvapi.Watcher, error) {
	return nil, errors.New("unused")
}
func (e *boundaryKVEngine) GrantLease(context.Context, time.Duration) (kvapi.Lease, error) {
	return e.lease, nil
}
func (e *boundaryKVEngine) SetWithLease(string, []byte, kvapi.LeaseID) (kvapi.Version, error) {
	return 0, e.setWithLeaseErr
}
func (e *boundaryKVEngine) SetIfAbsentWithLease(string, []byte, kvapi.LeaseID) (kvapi.Version, bool, error) {
	return 0, e.absentOK, e.absentErr
}

type boundaryKVTranscoder struct{}

func (boundaryKVTranscoder) Unmarshal(payload.Payload, any) error { return errors.New("unused") }
func (boundaryKVTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

func TestD08KVFailedSetRevokesLease(t *testing.T) {
	writeErr := errors.New("write failed")
	lease := &boundaryLease{}
	engine := &boundaryKVEngine{lease: lease, setWithLeaseErr: writeErr}
	s := NewStore(registry.ParseID("test:store"), "lease", engine, boundaryKVTranscoder{}, zap.NewNop())

	_, err := s.Put(context.Background(), registry.ParseID("app:key"), payload.NewPayload([]byte(`"value"`), payload.JSON), store.PutOptions{TTL: time.Minute})
	require.ErrorIs(t, err, writeErr)
	require.EqualValues(t, 1, lease.revokes.Load())
}

func TestD09KVAbsentConflictRevokesLease(t *testing.T) {
	lease := &boundaryLease{}
	engine := &boundaryKVEngine{lease: lease, absentOK: false}
	s := NewStore(registry.ParseID("test:store"), "lease", engine, boundaryKVTranscoder{}, zap.NewNop())

	_, err := s.Put(context.Background(), registry.ParseID("app:key"), payload.NewPayload([]byte(`"value"`), payload.JSON), store.PutOptions{OnlyIfAbsent: true, TTL: time.Minute})
	require.ErrorIs(t, err, store.ErrKeyExists)
	require.EqualValues(t, 1, lease.revokes.Load())
}
