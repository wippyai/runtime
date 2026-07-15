// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeSink struct{}

func (fakeSink) PreUpdate(int, string, string, int64, int, []any, []any, error) {}
func (fakeSink) Commit()                                                        {}
func (fakeSink) Rollback()                                                      {}

func TestCaptureRegistrySingleOwnerAndTokenGuard(t *testing.T) {
	const file = "/tmp/wippy-cdc-registry-test.db"

	a := fakeSink{}
	b := fakeSink{}

	tok, err := claimCapture(file, a)
	require.NoError(t, err)
	t.Cleanup(func() { releaseCapture(file, tok) })

	_, err = claimCapture(file, b)
	require.ErrorIs(t, err, errCaptureOwned, "a second owner must be refused")

	releaseCapture(file, tok+1000)
	_, err = claimCapture(file, b)
	require.ErrorIs(t, err, errCaptureOwned, "release with a stale token must not evict the owner")

	releaseCapture(file, tok)
	tok2, err := claimCapture(file, b)
	require.NoError(t, err, "after the real owner releases, a new owner can claim")
	require.NotEqual(t, tok, tok2)
	releaseCapture(file, tok2)
}
