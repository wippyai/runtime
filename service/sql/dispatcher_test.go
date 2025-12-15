package sql

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

type testReceiver struct {
	fn func(tag uint64, data any, err error)
}

func (r *testReceiver) CompleteYield(tag uint64, data any, err error) {
	if r.fn != nil {
		r.fn(tag, data, err)
	}
}

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT)`)
	require.NoError(t, err)

	return db
}

func TestDispatcher_RegisterAll(t *testing.T) {
	d := NewDispatcher()

	var registered []dispatcher.CommandID
	register := func(id dispatcher.CommandID, h dispatcher.Handler) {
		registered = append(registered, id)
		assert.NotNil(t, h)
	}

	d.RegisterAll(register)

	assert.Len(t, registered, 12)
	assert.Contains(t, registered, sqlapi.Query)
	assert.Contains(t, registered, sqlapi.Execute)
	assert.Contains(t, registered, sqlapi.Prepare)
	assert.Contains(t, registered, sqlapi.Begin)
	assert.Contains(t, registered, sqlapi.StmtQuery)
	assert.Contains(t, registered, sqlapi.StmtExecute)
	assert.Contains(t, registered, sqlapi.StmtClose)
	assert.Contains(t, registered, sqlapi.TxQuery)
	assert.Contains(t, registered, sqlapi.TxExecute)
	assert.Contains(t, registered, sqlapi.TxPrepare)
	assert.Contains(t, registered, sqlapi.TxCommit)
	assert.Contains(t, registered, sqlapi.TxRollback)
}

func TestDispatcher_Query(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	_, err := db.Exec(`INSERT INTO test (name) VALUES ('alice'), ('bob')`)
	require.NoError(t, err)

	d := NewDispatcher()
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	cmd := &sqlapi.QueryCmd{
		DB:    db,
		Query: "SELECT id, name FROM test ORDER BY id",
	}

	done := make(chan sqlapi.QueryResponse, 1)
	err = handlers[sqlapi.Query].Handle(context.Background(), cmd, 1, &testReceiver{fn: func(_ uint64, data any, _ error) {
		done <- data.(sqlapi.QueryResponse)
	}})
	require.NoError(t, err)

	select {
	case resp := <-done:
		assert.NoError(t, resp.Error)
		assert.Equal(t, []string{"id", "name"}, resp.Columns)
		assert.Len(t, resp.Rows, 2)
		assert.Equal(t, int64(1), resp.Rows[0][0])
		assert.Equal(t, "alice", resp.Rows[0][1])
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_Execute(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	d := NewDispatcher()
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	cmd := &sqlapi.ExecuteCmd{
		DB:     db,
		Query:  "INSERT INTO test (name) VALUES (?)",
		Params: []any{"charlie"},
	}

	done := make(chan sqlapi.ExecuteResponse, 1)
	err := handlers[sqlapi.Execute].Handle(context.Background(), cmd, 1, &testReceiver{fn: func(_ uint64, data any, _ error) {
		done <- data.(sqlapi.ExecuteResponse)
	}})
	require.NoError(t, err)

	select {
	case resp := <-done:
		assert.NoError(t, resp.Error)
		assert.Equal(t, int64(1), resp.LastInsertID)
		assert.Equal(t, int64(1), resp.RowsAffected)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_Transaction(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	d := NewDispatcher()
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	// Begin transaction
	beginCmd := &sqlapi.BeginCmd{DB: db}
	beginDone := make(chan sqlapi.BeginResponse, 1)
	err := handlers[sqlapi.Begin].Handle(context.Background(), beginCmd, 1, &testReceiver{fn: func(_ uint64, data any, _ error) {
		beginDone <- data.(sqlapi.BeginResponse)
	}})
	require.NoError(t, err)

	var tx *sql.Tx
	select {
	case resp := <-beginDone:
		assert.NoError(t, resp.Error)
		tx = resp.Tx
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	// Execute in transaction
	txExecCmd := &sqlapi.TxExecuteCmd{
		Tx:     tx,
		Query:  "INSERT INTO test (name) VALUES (?)",
		Params: []any{"dave"},
	}
	txExecDone := make(chan sqlapi.ExecuteResponse, 1)
	err = handlers[sqlapi.TxExecute].Handle(context.Background(), txExecCmd, 2, &testReceiver{fn: func(_ uint64, data any, _ error) {
		txExecDone <- data.(sqlapi.ExecuteResponse)
	}})
	require.NoError(t, err)

	select {
	case resp := <-txExecDone:
		assert.NoError(t, resp.Error)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	// Commit
	commitCmd := &sqlapi.TxCommitCmd{Tx: tx}
	commitDone := make(chan struct{}, 1)
	err = handlers[sqlapi.TxCommit].Handle(context.Background(), commitCmd, 3, &testReceiver{fn: func(_ uint64, _ any, err error) {
		assert.NoError(t, err)
		close(commitDone)
	}})
	require.NoError(t, err)

	select {
	case <-commitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	// Verify data was committed
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM test WHERE name = 'dave'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestDispatcher_PreparedStatement(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	d := NewDispatcher()
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	// Prepare statement
	prepCmd := &sqlapi.PrepareCmd{
		DB:    db,
		Query: "INSERT INTO test (name) VALUES (?)",
	}
	prepDone := make(chan sqlapi.PrepareResponse, 1)
	err := handlers[sqlapi.Prepare].Handle(context.Background(), prepCmd, 1, &testReceiver{fn: func(_ uint64, data any, _ error) {
		prepDone <- data.(sqlapi.PrepareResponse)
	}})
	require.NoError(t, err)

	var stmt *sql.Stmt
	select {
	case resp := <-prepDone:
		assert.NoError(t, resp.Error)
		stmt = resp.Stmt
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	// Execute prepared statement
	stmtExecCmd := &sqlapi.StmtExecuteCmd{
		Stmt:   stmt,
		Params: []any{"eve"},
	}
	stmtExecDone := make(chan sqlapi.ExecuteResponse, 1)
	err = handlers[sqlapi.StmtExecute].Handle(context.Background(), stmtExecCmd, 2, &testReceiver{fn: func(_ uint64, data any, _ error) {
		stmtExecDone <- data.(sqlapi.ExecuteResponse)
	}})
	require.NoError(t, err)

	select {
	case resp := <-stmtExecDone:
		assert.NoError(t, resp.Error)
		assert.Equal(t, int64(1), resp.RowsAffected)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	// Close statement
	closeCmd := &sqlapi.StmtCloseCmd{Stmt: stmt}
	closeDone := make(chan struct{}, 1)
	err = handlers[sqlapi.StmtClose].Handle(context.Background(), closeCmd, 3, &testReceiver{fn: func(_ uint64, _ any, err error) {
		assert.NoError(t, err)
		close(closeDone)
	}})
	require.NoError(t, err)

	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_ContextCancellation(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	d := NewDispatcher()
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	cmd := &sqlapi.QueryCmd{
		DB:    db,
		Query: "SELECT * FROM test",
	}

	completed := make(chan bool, 1)
	err := handlers[sqlapi.Query].Handle(ctx, cmd, 1, &testReceiver{fn: func(_ uint64, _ any, _ error) {
		completed <- true
	}})
	require.NoError(t, err)

	// Should not complete because context was cancelled
	select {
	case <-completed:
		t.Fatal("should not complete when context is cancelled")
	case <-time.After(100 * time.Millisecond):
		// Expected - no completion
	}
}
