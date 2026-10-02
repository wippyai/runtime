// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	process "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

type admissionTestProcess struct{}

func (admissionTestProcess) Init(context.Context, string, payload.Payloads) error { return nil }
func (admissionTestProcess) Step([]process.Event, *process.StepOutput) error      { return nil }
func (admissionTestProcess) Close()                                               {}

type admissionHost struct {
	mockHost
	accepts bool
}

func (h *admissionHost) AcceptsAdmission() bool { return h.accepts }

func testAdmission() *process.Admission {
	return &process.Admission{
		Factory: func() (process.Process, error) { return admissionTestProcess{}, nil },
		Meta:    process.Meta{Method: "main"},
	}
}

func TestManagerStartRejectsAdmissionForHostWithoutAdmission(t *testing.T) {
	for name, host := range map[string]process.Host{
		"no capability":     &mockHost{},
		"capability denied": &admissionHost{accepts: false},
	} {
		t.Run(name, func(t *testing.T) {
			node := newMockNode()
			_ = node.RegisterHost("host", host)

			_, err := NewManager(node, zap.NewNop()).Start(context.Background(), &process.Start{
				HostID:    "host",
				Source:    registry.NewID("eval.program", "abc"),
				Admission: testAdmission(),
			})
			require.ErrorIs(t, err, ErrAdmissionUnsupported)
		})
	}
}

func TestManagerStartForwardsAdmissionToAcceptingHost(t *testing.T) {
	node := newMockNode()
	host := &admissionHost{accepts: true}
	_ = node.RegisterHost("host", host)

	_, err := NewManager(node, zap.NewNop()).Start(context.Background(), &process.Start{
		HostID:    "host",
		Source:    registry.NewID("eval.program", "abc"),
		Admission: testAdmission(),
	})
	require.NoError(t, err)
	require.True(t, host.runCalled)
}
