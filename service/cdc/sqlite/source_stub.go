// SPDX-License-Identifier: MPL-2.0

//go:build !sqlite_preupdate_hook

package sqlite

func buildSource(_ sourceOptions) (managedSource, error) {
	return nil, ErrPreupdateTagRequired
}
