// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/service/temporal/internal/securitykeys"
	"github.com/wippyai/runtime/service/temporal/propagator"
	"go.uber.org/zap"
)

func TestSecuredWorkflowRejectsUnsignedContinuedMailbox(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	base, env := newRouterDefinition(t, nil)
	env.info.ContinuedExecutionRunID = "previous-run"
	header, err := propagator.AddSecurityToHeader(env.dc, nil, &propagator.SecurityPayload{
		Audience: "self-workflow", Actor: &propagator.ActorPayload{ID: "user"}, Scope: true,
	}, key)
	require.NoError(t, err)
	// A valid actor claim cannot authenticate independently added messages.
	header, err = propagator.AddMailboxToHeader(env.dc, header, []process.MailboxEnvelope{{
		Source: pid.PID{Node: "test", Host: "trusted", UniqID: "forged"}, Topic: "data", Payloads: payload.Payloads{payload.NewString("unsigned")},
	}}, "self-workflow", "first-run")
	require.NoError(t, err)
	ctx := process.WithFactory(base.execCtx, deliveryFactory{create: func() (process.Process, error) {
		return &recordingProcess{}, nil
	}})
	d := &Definition{id: registry.NewID("test", "mailbox_auth"), ctx: securitykeys.WithKeys(ctx, key), log: zap.NewNop()}
	d.Execute(&initializingEnvironment{env}, header, nil)
	t.Cleanup(d.Close)
	require.Len(t, env.completions, 1)
	require.ErrorContains(t, env.completions[0].err, "mailbox requires a valid signature")
}
