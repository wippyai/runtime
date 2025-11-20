package sqlite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	"go.uber.org/zap"
)

func TestSQLiteHistory_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	hist, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer hist.Close()

	head, err := hist.Head()
	require.NoError(t, err)
	assert.Equal(t, uint(0), head.ID())
}

func TestSQLiteHistory_SaveAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	hist, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer hist.Close()

	v0, err := hist.Head()
	require.NoError(t, err)

	v1 := version.FromParent(v0, 1)
	cs := registry.ChangeSet{
		{Kind: registry.Create, Entry: registry.Entry{ID: registry.ID{NS: "test", Name: "entry1"}}},
	}

	err = hist.Save(v1, cs, true)
	require.NoError(t, err)

	head, err := hist.Head()
	require.NoError(t, err)
	assert.Equal(t, uint(1), head.ID())

	retrieved, err := hist.Get(v1)
	require.NoError(t, err)
	assert.Len(t, retrieved, 1)
	assert.Equal(t, registry.Create, retrieved[0].Kind)
	assert.Equal(t, "test", retrieved[0].Entry.ID.NS)
}

func TestSQLiteHistory_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	hist1, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)

	v0, err := hist1.Head()
	require.NoError(t, err)

	v1 := version.FromParent(v0, 1)
	cs := registry.ChangeSet{
		{Kind: registry.Create, Entry: registry.Entry{ID: registry.ID{NS: "test", Name: "entry1"}}},
	}

	err = hist1.Save(v1, cs, true)
	require.NoError(t, err)
	hist1.Close()

	hist2, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer hist2.Close()

	head, err := hist2.Head()
	require.NoError(t, err)
	assert.Equal(t, uint(1), head.ID())

	versions, err := hist2.Versions()
	require.NoError(t, err)
	assert.Len(t, versions, 2)
}

func TestSQLiteHistory_Versions(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	hist, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer hist.Close()

	v0, err := hist.Head()
	require.NoError(t, err)

	v1 := version.FromParent(v0, 1)
	cs1 := registry.ChangeSet{
		{Kind: registry.Create, Entry: registry.Entry{ID: registry.ID{NS: "test", Name: "entry1"}}},
	}
	err = hist.Save(v1, cs1, true)
	require.NoError(t, err)

	v2 := version.FromParent(v1, 2)
	cs2 := registry.ChangeSet{
		{Kind: registry.Create, Entry: registry.Entry{ID: registry.ID{NS: "test", Name: "entry2"}}},
	}
	err = hist.Save(v2, cs2, true)
	require.NoError(t, err)

	versions, err := hist.Versions()
	require.NoError(t, err)
	assert.Len(t, versions, 3)
	assert.Equal(t, uint(0), versions[0].ID())
	assert.Equal(t, uint(1), versions[1].ID())
	assert.Equal(t, uint(2), versions[2].ID())
}

func TestSQLiteHistory_SetHead(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	hist, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer hist.Close()

	v0, err := hist.Head()
	require.NoError(t, err)

	v1 := version.FromParent(v0, 1)
	cs1 := registry.ChangeSet{
		{Kind: registry.Create, Entry: registry.Entry{ID: registry.ID{NS: "test", Name: "entry1"}}},
	}
	err = hist.Save(v1, cs1, false)
	require.NoError(t, err)

	v2 := version.FromParent(v1, 2)
	cs2 := registry.ChangeSet{
		{Kind: registry.Create, Entry: registry.Entry{ID: registry.ID{NS: "test", Name: "entry2"}}},
	}
	err = hist.Save(v2, cs2, true)
	require.NoError(t, err)

	head, err := hist.Head()
	require.NoError(t, err)
	assert.Equal(t, uint(2), head.ID())

	err = hist.SetHead(v1)
	require.NoError(t, err)

	head, err = hist.Head()
	require.NoError(t, err)
	assert.Equal(t, uint(1), head.ID())
}

func TestSQLiteHistory_NotFoundError(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	hist, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer hist.Close()

	v999 := version.New(999)
	_, err = hist.Get(v999)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "changeset not found")
}

func TestSQLiteHistory_DatabaseFileCreation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	_, err := os.Stat(dbPath)
	assert.True(t, os.IsNotExist(err))

	hist, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer hist.Close()

	_, err = os.Stat(dbPath)
	assert.NoError(t, err)
}
