// SPDX-License-Identifier: MPL-2.0

//go:build !sqlite_preupdate_hook

package sqlite

import (
	"context"
	"database/sql"
	"errors"

	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

var errObserverUnavailable = errors.New("sqlite committed-mutation observation is unavailable in this build")

// openSQLite uses the normal registered SQLite driver in builds without the
// optional pre-update hook. The SQL resource remains fully usable; CDC callers
// receive an explicit unsupported capability instead of a partially working
// capture source.
func openSQLite(_ context.Context, dsn string, _ ...int) (*sql.DB, sqlapi.CommittedMutationSource, error) {
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, nil, err
	}
	return db, nil, nil
}
