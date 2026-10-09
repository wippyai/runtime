// SPDX-License-Identifier: MPL-2.0

// Package sqliteconn shares physical SQLite connection initialization between
// SQL pools and registry history without replacing the registered driver.
package sqliteconn

import (
	"context"
	"database/sql"
	"database/sql/driver"

	"github.com/rqlite/go-sqlite3"
)

// Open returns a lazy pool with Wippy's connection policy applied to every
// physical connection, including replacements. The caller owns pool tuning.
func Open(dsn string) *sql.DB {
	return sql.OpenDB(&connector{driver: NewDriver(), dsn: dsn})
}

type connector struct {
	driver *sqlite3.SQLiteDriver
	dsn    string
}

func (c *connector) Connect(context.Context) (driver.Conn, error) {
	return c.driver.Open(c.dsn)
}

func (c *connector) Driver() driver.Driver { return c.driver }
