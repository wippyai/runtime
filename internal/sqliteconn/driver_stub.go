// SPDX-License-Identifier: MPL-2.0

//go:build !cgo

package sqliteconn

import "github.com/rqlite/go-sqlite3"

// NewDriver retains the driver's explicit unsupported-open error without CGO.
func NewDriver() *sqlite3.SQLiteDriver { return &sqlite3.SQLiteDriver{} }
