// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"

	config "github.com/wippyai/runtime/api/service/cdc"
)

func TestDriverKind(t *testing.T) {
	assert.Equal(t, config.SQLite, Driver{}.Kind())
	assert.Equal(t, config.SQLite, NewDriver().Kind())
}
