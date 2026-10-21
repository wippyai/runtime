// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	lua "github.com/wippyai/go-lua"
)

func TestHubCallErrorTreatsTransportCancelAsUnavailable(t *testing.T) {
	reset := connect.NewError(connect.CodeCanceled, errors.New("write: connection reset by peer"))

	l := lua.NewState()
	defer l.Close()
	l.SetContext(context.Background())
	err := hubCallError(l, reset)
	assert.Equal(t, lua.Unavailable, err.Kind())
	assert.Equal(t, "unavailable", err.Details()["code"])

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l.SetContext(ctx)
	err = hubCallError(l, reset)
	assert.Equal(t, lua.Canceled, err.Kind(), "a cancel from the caller stays a cancel")
}
