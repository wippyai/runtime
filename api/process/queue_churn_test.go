// SPDX-License-Identifier: MPL-2.0

package process

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/relay"
)

func TestOrdinaryMessageAdmissionReusesPackageSlice(t *testing.T) {
	q := NewEventQueue()
	gen := q.Generation()
	messages := []*relay.Message{{Topic: "ordinary"}, nil, {Topic: "another"}}
	pkg := &relay.Package{Messages: messages}
	allocs := testing.AllocsPerRun(1000, func() {
		if q.PushMessage(Event{Type: EventMessage, Data: pkg}, gen) != MessageAccepted {
			panic("ordinary message not admitted")
		}
		batch := q.Drain()
		if len(batch) != 1 {
			panic("ordinary message not delivered")
		}
		// Drain transfers ownership back before the next admission. Reuse the
		// same package rather than depending on sync.Pool retention under race.
	})
	require.Equal(t, float64(0), allocs)
	require.Same(t, &messages[0], &pkg.Messages[0])
	require.Equal(t, messages, pkg.Messages)
}
