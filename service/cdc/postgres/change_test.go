// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRowChangeRelation(t *testing.T) {
	assert.Equal(t, "public.accounts", RowChange{Schema: "public", Table: "accounts"}.Relation())
	assert.Equal(t, "accounts", RowChange{Table: "accounts"}.Relation())
}
