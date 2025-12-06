package supervisor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/registry"
	supervisorapi "github.com/wippyai/runtime/api/service/supervisor"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/internal/uniqid"
)

func TestNewService(t *testing.T) {
	pidGen := uniqid.NewPIDGenerator(uniqid.NewGenerator(), "test-node")
	id := registry.ID{NS: "test", Name: "svc"}
	config := supervisorapi.ServiceConfig{
		Process:   registry.ID{NS: "test", Name: "proc"},
		HostID:    "test-host",
		Lifecycle: supervisor.LifecycleConfig{},
	}

	svc := NewService(id, config, pidGen)

	assert.Equal(t, id, svc.id)
	assert.Equal(t, config, svc.config)
	assert.NotNil(t, svc.pidGen)
}

func TestService_ImplementsSupervisorService(_ *testing.T) {
	var _ supervisor.Service = (*Service)(nil)
}
