// SPDX-License-Identifier: MPL-2.0

package global

import (
	"encoding/binary"
	"fmt"

	"github.com/hashicorp/go-msgpack/v2/codec"
)

// SetJoinConfig configures join work before the service starts or handles joins.
func (s *Service) SetJoinConfig(cfg JoinConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || len(s.joinSlots) > 0 {
		return fmt.Errorf("cannot change join configuration while running")
	}
	s.joinConfig = cfg
	s.joinSlots = make(chan struct{}, cfg.MaxConcurrent)
	return nil
}

func (s *Service) acquireJoin() (JoinConfig, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.joinSlots == nil {
		s.joinConfig = DefaultJoinConfig()
		s.joinSlots = make(chan struct{}, s.joinConfig.MaxConcurrent)
	}
	slots := s.joinSlots
	select {
	case slots <- struct{}{}:
		return s.joinConfig, func() { <-slots }, nil
	default:
		return JoinConfig{}, nil, ErrJoinBusy
	}
}

// boundedJoinWriter bounds encoded length and buffer capacity. Encoder errors
// discard the entire response; no partial snapshot is returned.
type boundedJoinWriter struct {
	data     []byte
	limit    int
	exceeded bool
}

func (w *boundedJoinWriter) Write(p []byte) (int, error) {
	if len(p) > w.limit-len(w.data) {
		w.exceeded = true
		return 0, ErrJoinSnapshotTooLarge
	}
	needed := len(w.data) + len(p)
	if needed > cap(w.data) {
		capacity := cap(w.data)
		if capacity > w.limit-capacity {
			capacity = w.limit
		} else {
			capacity *= 2
		}
		if capacity < needed {
			capacity = needed
		}
		next := make([]byte, len(w.data), capacity)
		copy(next, w.data)
		w.data = next
	}
	w.data = append(w.data, p...)
	return len(p), nil
}
func encodeJoinSnapshot(snapshot *joinResponseEnvelope, limit int) ([]byte, error) {
	w := &boundedJoinWriter{limit: limit}
	if err := codec.NewEncoder(w, newMsgpackHandle()).Encode(snapshot); err != nil {
		if w.exceeded {
			return nil, ErrJoinSnapshotTooLarge
		}
		return nil, err
	}
	return w.data, nil
}

func (s *Service) joinPolicy() JoinConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.joinConfig.Timeout == 0 {
		return DefaultJoinConfig()
	}
	return s.joinConfig
}

// Decode the entries as raw bytes first: checking slice length after decoding
// would permit an oversized declared array to allocate before rejection.
func decodeJoinSnapshot(body []byte, cfg JoinConfig) (*joinResponseEnvelope, error) {
	if len(body) > cfg.MaxBytes {
		return nil, ErrJoinSnapshotTooLarge
	}
	// The envelope has exactly three fields. Bound its map before decoding so
	// attacker-supplied unknown keys cannot create an unbounded temporary map.
	if len(body) == 0 {
		return nil, fmt.Errorf("empty join snapshot")
	}
	fields := uint64(0)
	switch {
	case body[0] >= 0x80 && body[0] <= 0x8f:
		fields = uint64(body[0] & 0x0f)
	case body[0] == 0xde && len(body) >= 3:
		fields = uint64(binary.BigEndian.Uint16(body[1:3]))
	case body[0] == 0xdf && len(body) >= 5:
		fields = uint64(binary.BigEndian.Uint32(body[1:5]))
	default:
		return nil, fmt.Errorf("join snapshot must be a map")
	}
	if fields != 3 {
		return nil, fmt.Errorf("join snapshot requires entries, correlation and revision")
	}
	var envelope map[string]codec.Raw
	handle := newMsgpackHandle()
	handle.MaxInitLen = cfg.MaxEntries
	decoder := codec.NewDecoderBytes(body, handle)
	if err := decoder.Decode(&envelope); err != nil {
		return nil, err
	}
	if decoder.NumBytesRead() != len(body) {
		return nil, fmt.Errorf("trailing join snapshot data")
	}
	raw, present := envelope["en"]
	corr, corrPresent := envelope["c"]
	revision, revisionPresent := envelope["si"]
	if !present || !corrPresent || !revisionPresent {
		return nil, fmt.Errorf("incomplete join snapshot")
	}
	// The codec represents a raw MessagePack nil as a nil slice.
	if len(raw) == 0 {
		raw = []byte{0xc0}
	}
	var count uint64
	switch {
	case raw[0] == 0xc0:
	case raw[0] >= 0x90 && raw[0] <= 0x9f:
		count = uint64(raw[0] & 0x0f)
	case raw[0] == 0xdc && len(raw) >= 3:
		count = uint64(binary.BigEndian.Uint16(raw[1:3]))
	case raw[0] == 0xdd && len(raw) >= 5:
		count = uint64(binary.BigEndian.Uint32(raw[1:5]))
	default:
		return nil, fmt.Errorf("join snapshot entries must be an array")
	}
	if count > uint64(cfg.MaxEntries) {
		return nil, ErrJoinSnapshotTooLarge
	}
	out := &joinResponseEnvelope{}
	if len(corr) == 0 || len(revision) == 0 {
		return nil, fmt.Errorf("null join correlation or revision")
	}
	if err := codec.NewDecoderBytes(corr, handle).Decode(&out.CorrID); err != nil {
		return nil, err
	}
	if err := codec.NewDecoderBytes(revision, handle).Decode(&out.StrongIndex); err != nil {
		return nil, err
	}
	if err := codec.NewDecoderBytes(raw, handle).Decode(&out.Entries); err != nil {
		return nil, err
	}
	return out, nil
}
