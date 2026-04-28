// SPDX-License-Identifier: MPL-2.0

package kvraft

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/kv"
)

// spaceKV is a thin per-space wrapper that targets the shared Service.
// All methods route through the Service which goes through Raft Apply for
// writes and reads from the local FSM replica with an optional barrier.
type spaceKV struct {
	svc  *Service
	name string
}

func (s *spaceKV) Mode() kv.Mode { return kv.ModeRaft }
func (s *spaceKV) Name() string  { return s.name }

func (s *spaceKV) Get(_ context.Context, key string) (kv.Value, error) {
	if s.svc.stopped.Load() {
		return kv.Value{}, kv.ErrSpaceClosed
	}
	if err := s.svc.readBarrier(); err != nil {
		return kv.Value{}, err
	}
	value, version, ttl, ok := s.svc.cfg.FSM.State().get(shardKey{Space: s.name, Key: key}, nowMs())
	if !ok {
		return kv.Value{}, kv.ErrKeyNotFound
	}
	v := kv.Value{Data: value, Version: version}
	if ttl > 0 {
		v.TTL = time.UnixMilli(ttl)
	}
	return v, nil
}

func (s *spaceKV) Put(_ context.Context, key string, data []byte, opts ...kv.PutOption) error {
	if s.svc.stopped.Load() {
		return kv.ErrSpaceClosed
	}
	o := kv.CollectPutOptions(opts)
	cmd := &Command{
		Type:         CmdPut,
		Space:        s.name,
		Key:          key,
		Value:        data,
		ExpectAbsent: o.ExpectAbsent,
	}
	if o.HasExpectVersion {
		cmd.ExpectVersion = o.ExpectVersion
	}
	if o.TTL > 0 {
		cmd.TTL = time.Now().Add(o.TTL).UnixMilli()
	}

	// Pre-touch the watch hub so the apply callback (which fires before
	// Apply returns to us) finds it. Idempotent.
	s.svc.lookupHub(s.name, true)

	_, err := s.svc.applyCommand(cmd)
	return err
}

func (s *spaceKV) Delete(_ context.Context, key string) error {
	if s.svc.stopped.Load() {
		return kv.ErrSpaceClosed
	}
	cmd := &Command{Type: CmdDelete, Space: s.name, Key: key}
	s.svc.lookupHub(s.name, true)
	_, err := s.svc.applyCommand(cmd)
	return err
}

func (s *spaceKV) CompareAndSwap(_ context.Context, key string, expected, newVal []byte) error {
	if s.svc.stopped.Load() {
		return kv.ErrSpaceClosed
	}
	cmd := &Command{
		Type:        CmdCAS,
		Space:       s.name,
		Key:         key,
		Value:       newVal,
		ExpectValue: expected,
	}
	s.svc.lookupHub(s.name, true)
	_, err := s.svc.applyCommand(cmd)
	return err
}

func (s *spaceKV) Watch(ctx context.Context, prefix string) (<-chan kv.Event, error) {
	if s.svc.stopped.Load() {
		return nil, kv.ErrSpaceClosed
	}
	hub := s.svc.lookupHub(s.name, true)
	ch, _ := hub.Subscribe(ctx, prefix)
	return ch, nil
}

func (s *spaceKV) Scan(_ context.Context, start, end string, fn func(string, kv.Value) bool) error {
	if s.svc.stopped.Load() {
		return kv.ErrSpaceClosed
	}
	if err := s.svc.readBarrier(); err != nil {
		return err
	}
	stop := false
	s.svc.cfg.FSM.State().scan(s.name, start, end, nowMs(), func(key string, value []byte, version uint64) bool {
		if stop {
			return false
		}
		if !fn(key, kv.Value{Data: value, Version: version}) {
			stop = true
			return false
		}
		return true
	})
	return nil
}

func (s *spaceKV) Close() error {
	// Closing a space doesn't invalidate the underlying raft node; just
	// closes the watch hub for that space if it exists.
	s.svc.hubMu.Lock()
	if h, ok := s.svc.hub[s.name]; ok {
		h.Close()
		delete(s.svc.hub, s.name)
	}
	s.svc.hubMu.Unlock()
	return nil
}

var _ kv.KV = (*spaceKV)(nil)
