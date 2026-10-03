// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTerminationCauseOption(t *testing.T) {
	base := context.Background()
	require.Nil(t, TerminationCause(base))
	ctx := WithTerminationCause(base, ErrOwnerEnded)
	require.ErrorIs(t, TerminationCause(ctx), ErrOwnerEnded)
	require.NoError(t, ctx.Err())
	require.Nil(t, TerminationCause(WithTerminationCause(ctx, nil)))
}
