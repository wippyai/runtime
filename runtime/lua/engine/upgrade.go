package engine

import (
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	lua "github.com/wippyai/go-lua"
)

// UpgradeRequest is yielded to request process upgrade.
// Engine detects this and signals StepUpgrade to the worker.
type UpgradeRequest struct {
	Source registry.ID      // target definition (empty = same definition)
	Input  payload.Payloads // args for new process
}

func (r *UpgradeRequest) String() string       { return "<upgrade_request>" }
func (r *UpgradeRequest) Type() lua.LValueType { return lua.LTUserData }
