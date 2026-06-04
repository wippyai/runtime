// SPDX-License-Identifier: MPL-2.0

// Package multiplex routes a single hashicorp/raft FSM slot across more than
// one logical state machine so the cluster carries exactly one raft instance.
// The primary FSM (the global name registry) owns untagged commands; the
// optional kv FSM owns commands prefixed with KVDomain.
package multiplex

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	hraft "github.com/hashicorp/raft"
)

// KVDomain tags a kv subcommand. It sits outside the byte range a msgpack
// map header can start with (0x80-0x8f, 0xde, 0xdf), which is how the global
// registry encodes every command, so an untagged command is unambiguously a
// primary-FSM command and existing call sites need no change. Guarded by a
// test that asserts no global command begins with this byte.
const KVDomain byte = 0xB2

// primaryDomain and kvDomain tag snapshot sections.
const (
	primaryDomain byte = 0x01
	kvDomain      byte = 0x02
)

var snapshotMagic = [4]byte{'W', 'K', 'V', 'R'}

const snapshotVersion byte = 1

// FSM is the hraft.FSM installed on the single raft node. It dispatches by the
// leading command byte and frames a combined snapshot of both sub-FSMs.
type FSM struct {
	primary hraft.FSM
	kv      hraft.FSM
}

// New builds a router. kv may be nil, in which case the router is transparent:
// every command goes to primary and snapshots carry only the primary section.
func New(primary, kv hraft.FSM) *FSM {
	return &FSM{primary: primary, kv: kv}
}

// Apply routes one log entry. A kv-tagged entry is forwarded to the kv FSM with
// the domain byte stripped; everything else goes to primary unchanged. The log
// is shallow-copied so the canonical entry the raft library retains is not
// mutated.
func (f *FSM) Apply(log *hraft.Log) any {
	if f.kv != nil && len(log.Data) > 0 && log.Data[0] == KVDomain {
		sub := *log
		sub.Data = log.Data[1:]
		return f.kv.Apply(&sub)
	}
	return f.primary.Apply(log)
}

// Snapshot captures both sub-FSMs under raft's Apply/Snapshot serialization, so
// the pair is internally consistent.
func (f *FSM) Snapshot() (hraft.FSMSnapshot, error) {
	ps, err := f.primary.Snapshot()
	if err != nil {
		return nil, err
	}
	var ks hraft.FSMSnapshot
	if f.kv != nil {
		ks, err = f.kv.Snapshot()
		if err != nil {
			ps.Release()
			return nil, err
		}
	}
	return &snapshot{primary: ps, kv: ks}, nil
}

// Restore reads a framed snapshot. A stream that does not begin with the magic
// is a legacy bare-primary snapshot (written before the router existed) and is
// forwarded whole to the primary FSM, which upgrades an existing cluster in
// place. Downgrade is therefore one-way: an old binary cannot read a framed
// snapshot.
func (f *FSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()

	var magic [4]byte
	n, err := io.ReadFull(rc, magic[:])
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return fmt.Errorf("multiplex restore: read magic: %w", err)
	}
	read := magic[:n]

	if n < 4 || magic != snapshotMagic {
		legacy := io.NopCloser(io.MultiReader(bytes.NewReader(read), rc))
		if err := f.primary.Restore(legacy); err != nil {
			return err
		}
		// A legacy bare-primary snapshot carries no kv section, so reset the kv
		// FSM rather than leave it holding stale state.
		return f.resetUnseenKV(false)
	}

	var ver [1]byte
	if _, err := io.ReadFull(rc, ver[:]); err != nil {
		return fmt.Errorf("multiplex restore: read version: %w", err)
	}
	if ver[0] != snapshotVersion {
		return fmt.Errorf("multiplex restore: unsupported snapshot version %d", ver[0])
	}

	seenKV := false
	for {
		var hdr [9]byte
		_, err := io.ReadFull(rc, hdr[:])
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("multiplex restore: read section header: %w", err)
		}
		domain := hdr[0]
		length := binary.BigEndian.Uint64(hdr[1:])
		section := make([]byte, length)
		if _, err := io.ReadFull(rc, section); err != nil {
			return fmt.Errorf("multiplex restore: read section %d: %w", domain, err)
		}

		var target hraft.FSM
		switch domain {
		case primaryDomain:
			target = f.primary
		case kvDomain:
			target = f.kv
			seenKV = true
		}
		if target == nil {
			continue
		}
		if err := target.Restore(io.NopCloser(bytes.NewReader(section))); err != nil {
			return fmt.Errorf("multiplex restore: restore section %d: %w", domain, err)
		}
	}
	return f.resetUnseenKV(seenKV)
}

// resetUnseenKV clears the kv FSM when the restored snapshot carried no kv
// section — a legacy bare-primary snapshot, or one written by a node with kv
// disabled. Raft calls Restore to REPLACE all state, so a child whose section is
// absent must be reset to empty; otherwise it keeps stale entries after an
// InstallSnapshot and diverges from the rest of the cluster. An empty stream is
// the kv FSM's reset-to-empty contract. The primary section is always written,
// so only the kv child can be absent.
func (f *FSM) resetUnseenKV(seenKV bool) error {
	if f.kv == nil || seenKV {
		return nil
	}
	if err := f.kv.Restore(io.NopCloser(bytes.NewReader(nil))); err != nil {
		return fmt.Errorf("multiplex restore: reset kv: %w", err)
	}
	return nil
}

// snapshot frames the child snapshots. Each child's Persist is captured into a
// buffer so the section can be length-prefixed; raft snapshots are bounded
// (name registry + kv working set), so buffering in memory is acceptable.
type snapshot struct {
	primary hraft.FSMSnapshot
	kv      hraft.FSMSnapshot
}

func (s *snapshot) Persist(sink hraft.SnapshotSink) error {
	if _, err := sink.Write(snapshotMagic[:]); err != nil {
		return s.fail(sink, err)
	}
	if _, err := sink.Write([]byte{snapshotVersion}); err != nil {
		return s.fail(sink, err)
	}
	if err := writeSection(sink, primaryDomain, s.primary); err != nil {
		return s.fail(sink, err)
	}
	if s.kv != nil {
		if err := writeSection(sink, kvDomain, s.kv); err != nil {
			return s.fail(sink, err)
		}
	}
	return sink.Close()
}

func (s *snapshot) fail(sink hraft.SnapshotSink, err error) error {
	_ = sink.Cancel()
	return err
}

func (s *snapshot) Release() {
	s.primary.Release()
	if s.kv != nil {
		s.kv.Release()
	}
}

// writeSection persists one child snapshot into a buffer, then writes a
// domain + length-prefixed section to the sink.
func writeSection(sink hraft.SnapshotSink, domain byte, child hraft.FSMSnapshot) error {
	var buf bytes.Buffer
	if err := child.Persist(&bufferSink{Buffer: &buf, id: sink.ID()}); err != nil {
		return err
	}
	var hdr [9]byte
	hdr[0] = domain
	binary.BigEndian.PutUint64(hdr[1:], uint64(buf.Len()))
	if _, err := sink.Write(hdr[:]); err != nil {
		return err
	}
	_, err := sink.Write(buf.Bytes())
	return err
}

// bufferSink adapts a bytes.Buffer to hraft.SnapshotSink so a child FSM's
// Persist can write into memory for framing.
type bufferSink struct {
	*bytes.Buffer
	id string
}

func (b *bufferSink) Close() error  { return nil }
func (b *bufferSink) ID() string    { return b.id }
func (b *bufferSink) Cancel() error { return nil }

var _ hraft.FSM = (*FSM)(nil)
var _ hraft.FSMSnapshot = (*snapshot)(nil)
var _ hraft.SnapshotSink = (*bufferSink)(nil)
