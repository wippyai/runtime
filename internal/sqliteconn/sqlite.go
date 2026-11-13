// SPDX-License-Identifier: MPL-2.0

// Package sqliteconn shares physical SQLite connection initialization and WAL
// checkpointing between SQL pools and registry history without replacing the
// registered driver.
package sqliteconn

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net/url"
	"strings"

	"github.com/rqlite/go-sqlite3"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

// Connector opens physical connections with Wippy's connection policy. A
// connector for a writable database file owns a background checkpointer that
// Attach starts against the connector's pool; database/sql closes the
// connector, and so stops the checkpointer, from DB.Close.
type Connector struct {
	driver       *sqlite3.SQLiteDriver
	checkpointer *checkpointer
	dsn          string
}

// NewConnector returns a connector for dsn. Writable file databases get a
// background checkpointer configured by checkpoint; in-memory and read-only
// databases have no WAL to checkpoint and get none. The caller opens the pool
// over the connector (or a wrapper delegating to it) and passes it to Attach.
func NewConnector(dsn string, checkpoint sqlapi.SQLiteCheckpointConfig) (*Connector, error) {
	c := &Connector{driver: NewDriver(), dsn: dsn}
	writable, err := writableFile(dsn)
	if err != nil {
		return nil, err
	}
	if !writable {
		return c, nil
	}
	cp, err := newCheckpointer(c, checkpoint)
	if err != nil {
		return nil, err
	}
	c.checkpointer = cp
	return c, nil
}

// Open returns a lazy pool over NewConnector with its checkpointer attached.
// The caller owns pool tuning and must Close the pool to stop the
// checkpointer.
func Open(dsn string, checkpoint sqlapi.SQLiteCheckpointConfig) (*sql.DB, error) {
	c, err := NewConnector(dsn, checkpoint)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(c)
	c.Attach(db)
	return db, nil
}

// Attach starts the checkpointer against pool, the writer pool opened over
// this connector. It escalates bounded-WAL checkpoints through pool so they
// take a writer turn. Attach is a no-op for connectors without a checkpointer.
func (c *Connector) Attach(pool *sql.DB) {
	if c.checkpointer != nil {
		c.checkpointer.start(pool)
	}
}

// Connect opens one physical connection.
func (c *Connector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.driver.Open(c.dsn)
	if err == nil && c.checkpointer != nil {
		c.checkpointer.opened.Store(true)
	}
	return conn, err
}

// Driver returns the pool-owned driver.
func (c *Connector) Driver() driver.Driver { return c.driver }

// Close stops the background checkpointer after a final checkpoint.
// database/sql calls it from DB.Close.
func (c *Connector) Close() error {
	if c.checkpointer == nil {
		return nil
	}
	return c.checkpointer.Close()
}

// writableFile reports whether dsn names a database file opened for writing,
// following go-sqlite3's DSN rules.
func writableFile(dsn string) (bool, error) {
	path, query, _ := strings.Cut(dsn, "?")
	path = strings.TrimPrefix(path, "file:")
	if path == "" || path == ":memory:" {
		return false, nil
	}
	params, err := url.ParseQuery(query)
	if err != nil {
		return false, fmt.Errorf("parse sqlite DSN parameters: %w", err)
	}
	switch params.Get("mode") {
	case "memory", "ro":
		return false, nil
	}
	return params.Get("immutable") != "1", nil
}
