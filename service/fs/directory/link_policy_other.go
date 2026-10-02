//go:build !unix

// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"errors"
	"os"
)

// Ownership/ACL evidence is unavailable here: owner_safe remains contained.
const ownerSafeSupported = false

func (d *FS) openOwnerSafe(name string) (*os.File, error) { return nil, errors.ErrUnsupported }
