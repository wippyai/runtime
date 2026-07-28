// SPDX-License-Identifier: MPL-2.0

package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

type boundaryBaseStore struct {
	value    payload.Payload
	getErr   error
	getCalls int
	setCalls int
}

func (s *boundaryBaseStore) Get(context.Context, registry.ID) (payload.Payload, error) {
	s.getCalls++
	return s.value, s.getErr
}
func (s *boundaryBaseStore) Set(context.Context, Entry) error             { s.setCalls++; return nil }
func (*boundaryBaseStore) Delete(context.Context, registry.ID) error      { return nil }
func (*boundaryBaseStore) Has(context.Context, registry.ID) (bool, error) { return false, nil }

type boundaryAtomicStore struct {
	casErr         error
	versionedErr   error
	setIfAbsentErr error
	*boundaryBaseStore
	casKey          registry.ID
	casEntry        Entry
	setIfAbsentArg  Entry
	versioned       VersionedEntry
	versionedCalls  int
	casCalls        int
	setIfAbsentCall int
	casExpected     Version
	casOK           bool
	setIfAbsentOK   bool
}

func (s *boundaryAtomicStore) GetVersioned(context.Context, registry.ID) (VersionedEntry, error) {
	s.versionedCalls++
	return s.versioned, s.versionedErr
}
func (s *boundaryAtomicStore) CompareAndSwap(_ context.Context, key registry.ID, expected Version, entry Entry) (bool, error) {
	s.casCalls++
	s.casKey = key
	s.casExpected = expected
	s.casEntry = entry
	return s.casOK, s.casErr
}
func (s *boundaryAtomicStore) SetIfAbsent(_ context.Context, entry Entry) (bool, error) {
	s.setIfAbsentCall++
	s.setIfAbsentArg = entry
	return s.setIfAbsentOK, s.setIfAbsentErr
}

type boundaryScannerStore struct {
	scanErr error
	*boundaryBaseStore
	scanOpts  ScanOptions
	entries   []Entry
	scanCalls int
}

func (s *boundaryScannerStore) Scan(_ context.Context, opts ScanOptions, fn func(Entry) bool) error {
	s.scanCalls++
	s.scanOpts = opts
	for _, entry := range s.entries {
		if !fn(entry) {
			break
		}
	}
	return s.scanErr
}

type boundaryEntryStore struct {
	entryErr error
	*boundaryAtomicStore
	entry      VersionedEntry
	entryCalls int
}

func (s *boundaryEntryStore) Entry(context.Context, registry.ID) (VersionedEntry, error) {
	s.entryCalls++
	return s.entry, s.entryErr
}

func newBoundaryAtomicStore() *boundaryAtomicStore {
	return &boundaryAtomicStore{boundaryBaseStore: &boundaryBaseStore{}}
}

func TestA12PutIfAbsentSuccessReadsVersion(t *testing.T) {
	key := registry.ParseID("boundary:insert")
	input := payload.NewPayload("input-value", payload.String)
	stored := VersionedEntry{Entry: Entry{Key: key, Value: payload.NewPayload("stored-value", payload.JSON), TTL: 9 * time.Second}, Version: 47}
	s := newBoundaryAtomicStore()
	s.setIfAbsentOK = true
	s.versioned = stored

	got, err := PutEntry(context.Background(), s, key, input, PutOptions{OnlyIfAbsent: true, TTL: 9 * time.Second})

	require.NoError(t, err)
	assert.Equal(t, stored, got)
	assert.Equal(t, 1, s.setIfAbsentCall)
	assert.Equal(t, Entry{Key: key, Value: input, TTL: 9 * time.Second}, s.setIfAbsentArg)
	assert.Equal(t, 1, s.versionedCalls)
}

func TestA13PutIfAbsentConflictMapping(t *testing.T) {
	s := newBoundaryAtomicStore()

	got, err := PutEntry(context.Background(), s, registry.ParseID("boundary:existing"), payload.New("value"), PutOptions{OnlyIfAbsent: true})

	assert.Equal(t, VersionedEntry{}, got)
	assert.ErrorIs(t, err, ErrKeyExists)
	assert.Equal(t, 1, s.setIfAbsentCall)
	assert.Zero(t, s.versionedCalls)
}

func TestA14PutIfAbsentBackendError(t *testing.T) {
	backendErr := errors.New("set-if-absent backend failure")
	s := newBoundaryAtomicStore()
	s.setIfAbsentErr = backendErr

	got, err := PutEntry(context.Background(), s, registry.ParseID("boundary:error"), payload.New("value"), PutOptions{OnlyIfAbsent: true})

	assert.Equal(t, VersionedEntry{}, got)
	assert.Same(t, backendErr, err)
	assert.Equal(t, 1, s.setIfAbsentCall)
	assert.Zero(t, s.versionedCalls)
}

func TestA15CASSuccessReadsMetadata(t *testing.T) {
	key := registry.ParseID("boundary:cas")
	stored := VersionedEntry{Entry: Entry{Key: key, Value: payload.New("stored-current"), TTL: time.Minute}, Version: 12}
	s := newBoundaryAtomicStore()
	s.casOK = true
	s.versioned = stored

	got, err := PutEntry(context.Background(), s, key, payload.New("replacement"), PutOptions{HasVersion: true, Version: 11, TTL: time.Minute})

	require.NoError(t, err)
	assert.Equal(t, stored, got)
	assert.Equal(t, 1, s.casCalls)
	assert.Equal(t, 1, s.versionedCalls)
}

func TestA16CASMismatchMapping(t *testing.T) {
	s := newBoundaryAtomicStore()

	got, err := PutEntry(context.Background(), s, registry.ParseID("boundary:mismatch"), payload.New("replacement"), PutOptions{HasVersion: true, Version: 3})

	assert.Equal(t, VersionedEntry{}, got)
	assert.ErrorIs(t, err, ErrVersionMismatch)
	assert.Equal(t, 1, s.casCalls)
	assert.Zero(t, s.versionedCalls)
}

func TestA17CASBackendErrorAndArguments(t *testing.T) {
	backendErr := errors.New("cas backend failure")
	key := registry.ParseID("literal:key")
	value := payload.NewPayload(map[string]any{"literal": "value"}, payload.JSON)
	s := newBoundaryAtomicStore()
	s.casErr = backendErr

	got, err := PutEntry(context.Background(), s, key, value, PutOptions{HasVersion: true, Version: 29, TTL: 37 * time.Second})

	assert.Equal(t, VersionedEntry{}, got)
	assert.Same(t, backendErr, err)
	assert.Equal(t, 1, s.casCalls)
	assert.Equal(t, key, s.casKey)
	assert.Equal(t, Version(29), s.casExpected)
	assert.Equal(t, Entry{Key: key, Value: value, TTL: 37 * time.Second}, s.casEntry)
	assert.Zero(t, s.versionedCalls)
}

func TestA18ListFallbackSortCursorLimit(t *testing.T) {
	s := &boundaryScannerStore{
		boundaryBaseStore: &boundaryBaseStore{},
		entries: []Entry{
			{Key: registry.ParseID("scope:d"), Value: payload.New("D")},
			{Key: registry.ParseID("scope:a"), Value: payload.New("A")},
			{Key: registry.ParseID("scope:c"), Value: payload.New("C")},
			{Key: registry.ParseID("scope:b"), Value: payload.New("B")},
		},
	}

	page, err := ListEntries(context.Background(), s, ListOptions{Prefix: "scope:", After: "scope:a", Limit: 2})

	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	assert.Equal(t, "scope:b", page.Items[0].Key.String())
	assert.Equal(t, "B", page.Items[0].Value.Data())
	assert.Equal(t, "scope:c", page.Items[1].Key.String())
	assert.Equal(t, "C", page.Items[1].Value.Data())
	assert.Equal(t, "scope:c", page.Cursor)
	assert.True(t, page.HasMore)
}

func TestA19ListForwardsOnlyPrefix(t *testing.T) {
	s := &boundaryScannerStore{boundaryBaseStore: &boundaryBaseStore{}}

	_, err := ListEntries(context.Background(), s, ListOptions{Prefix: "tenant:", After: "tenant:k9", Limit: 7})

	require.NoError(t, err)
	assert.Equal(t, 1, s.scanCalls)
	assert.Equal(t, ScanOptions{Prefix: "tenant:"}, s.scanOpts)
}

func TestA20ListFailureReturnsNoPage(t *testing.T) {
	backendErr := errors.New("scan backend failure")
	s := &boundaryScannerStore{
		boundaryBaseStore: &boundaryBaseStore{},
		entries:           []Entry{{Key: registry.ParseID("scope:candidate"), Value: payload.New("candidate")}},
		scanErr:           backendErr,
	}

	page, err := ListEntries(context.Background(), s, ListOptions{Prefix: "scope:", Limit: 5})

	assert.Same(t, backendErr, err)
	assert.Equal(t, Page{}, page)
}

func TestA21ReadEntryReaderPrecedence(t *testing.T) {
	atomic := newBoundaryAtomicStore()
	stored := VersionedEntry{Entry: Entry{Key: registry.ParseID("scope:reader"), Value: payload.New("entry-reader")}, Version: 71}
	s := &boundaryEntryStore{boundaryAtomicStore: atomic, entry: stored}

	got, err := ReadEntry(context.Background(), s, stored.Key)

	require.NoError(t, err)
	assert.Equal(t, stored, got)
	assert.Equal(t, 1, s.entryCalls)
	assert.Zero(t, atomic.versionedCalls)
	assert.Zero(t, atomic.getCalls)
}

func TestA22ReadAtomicPrecedence(t *testing.T) {
	stored := VersionedEntry{Entry: Entry{Key: registry.ParseID("scope:atomic"), Value: payload.New("versioned")}, Version: 18}
	s := newBoundaryAtomicStore()
	s.versioned = stored
	s.value = payload.New("base")

	got, err := ReadEntry(context.Background(), s, stored.Key)

	require.NoError(t, err)
	assert.Equal(t, stored, got)
	assert.Equal(t, 1, s.versionedCalls)
	assert.Zero(t, s.getCalls)
}

func TestA23ReadBaseStoreFallback(t *testing.T) {
	key := registry.ParseID("scope:base")
	value := payload.NewPayload("base-value", payload.String)
	s := &boundaryBaseStore{value: value}

	got, err := ReadEntry(context.Background(), s, key)

	require.NoError(t, err)
	assert.Equal(t, key, got.Key)
	assert.Equal(t, value, got.Value)
	assert.Zero(t, got.TTL)
	assert.Zero(t, got.Version)
	assert.Equal(t, 1, s.getCalls)
}

func TestA24ReadBackendError(t *testing.T) {
	backendErr := errors.New("entry reader failure")
	atomic := newBoundaryAtomicStore()
	atomic.value = payload.New("base")
	s := &boundaryEntryStore{boundaryAtomicStore: atomic, entryErr: backendErr}

	got, err := ReadEntry(context.Background(), s, registry.ParseID("scope:failure"))

	assert.Equal(t, VersionedEntry{}, got)
	assert.Same(t, backendErr, err)
	assert.Equal(t, 1, s.entryCalls)
	assert.Zero(t, atomic.versionedCalls)
	assert.Zero(t, atomic.getCalls)
}
