// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuoteReplicationSlotNameUsesServerGrammar(t *testing.T) {
	for _, name := range []string{"events_2026_08", "slot0", strings.Repeat("x", postgresIdentifierMaxBytes)} {
		quoted, err := quoteReplicationSlotName(name)
		require.NoError(t, err, "name %q", name)
		assert.Equal(t, `"`+name+`"`, quoted)
	}

	for _, name := range []string{
		"", "Events", "events-name", `events"name`, "events name",
		"événements", "slot\nname", string([]byte{0xff}),
		strings.Repeat("x", postgresIdentifierMaxBytes+1),
	} {
		_, err := quoteReplicationSlotName(name)
		assert.ErrorIs(t, err, ErrInvalidIdentifier, "name %q", name)
	}
}

func TestQuotePostgresIdentifierUsesServerIdentifierQuoting(t *testing.T) {
	quoted, err := quotePostgresIdentifier(`publication"name`, "publication")
	require.NoError(t, err)
	assert.Equal(t, `"publication""name"`, quoted)

	literal, err := quotePostgresLiteral(`publication'name`, "publication")
	require.NoError(t, err)
	assert.Equal(t, `'publication''name'`, literal)

	qualified, err := quoteQualifiedIdent("public.accounts")
	require.NoError(t, err)
	assert.Equal(t, `"public"."accounts"`, qualified)
}

func TestQuoteQualifiedIdentRejectsMalformedTable(t *testing.T) {
	for _, table := range []string{"public.accounts.extra", ".accounts", "public."} {
		_, err := quoteQualifiedIdent(table)
		assert.ErrorIs(t, err, ErrInvalidIdentifier, "table %q", table)
	}
}
