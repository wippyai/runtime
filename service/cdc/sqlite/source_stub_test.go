// SPDX-License-Identifier: MPL-2.0

//go:build !sqlite_preupdate_hook

package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildSourceRequiresTag(t *testing.T) {
	src, err := buildSource(sourceOptions{name: "x"})
	assert.Nil(t, src)
	assert.ErrorIs(t, err, ErrPreupdateTagRequired)
}
