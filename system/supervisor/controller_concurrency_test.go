// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestY12ControllerDetailCloseInterleaving(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	details := make(chan any)
	exited := make(chan any)
	controller := &Controller{state: newInternalState(), ops: make(chan ctrlOp, 1)}

	go controller.monitor(ctx, exited, details)
	deliveredAndClosed := make(chan struct{})
	go func() {
		details <- "last-detail"
		close(details)
		close(deliveredAndClosed)
	}()

	<-deliveredAndClosed
	<-exited
	require.Equal(t, "last-detail", controller.State().Details)
}
