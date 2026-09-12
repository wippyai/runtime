// SPDX-License-Identifier: MPL-2.0
package internode

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoundedQueueGrowthMatchesFIFOAndRetryModel(t *testing.T) {
	for _, capacity := range []int{1, 3, 8, 17, 64, 1024} {
		t.Run(fmt.Sprint(capacity), func(t *testing.T) {
			queue := newClassQueue(capacity)
			require.Empty(t, queue.buf, "idle queue should not allocate backing storage")
			var model []string
			random := rand.New(rand.NewPCG(7, uint64(capacity)))
			for step := range 5000 {
				switch random.IntN(3) {
				case 0, 1:
					value := fmt.Sprint(step)
					front := random.IntN(2) == 0
					accepted := false
					if front {
						accepted = queue.pushFront([]byte(value))
					} else {
						accepted = queue.pushNewest([]byte(value))
					}
					require.Equal(t, len(model) < capacity, accepted)
					if accepted {
						if front {
							model = append([]string{value}, model...)
						} else {
							model = append(model, value)
						}
					}
				case 2:
					value, ok := queue.pop()
					require.Equal(t, len(model) != 0, ok)
					if ok {
						require.Equal(t, model[0], string(value.Data))
						model = model[1:]
					}
				}
				require.Equal(t, len(model), queue.len())
				require.LessOrEqual(t, len(queue.buf), capacity)
			}
			for _, expected := range model {
				value, ok := queue.pop()
				require.True(t, ok)
				require.Equal(t, expected, string(value.Data))
			}
			queue.reset()
			require.Zero(t, queue.len())
			require.True(t, queue.pushFront([]byte("after-reset")))
			value, _ := queue.pop()
			require.Equal(t, "after-reset", string(value.Data))
		})
	}
}
