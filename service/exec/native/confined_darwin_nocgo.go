// SPDX-License-Identifier: MPL-2.0

//go:build darwin && !cgo

package native

import execapi "github.com/wippyai/runtime/api/service/exec"

func validateConfinementHost(_ *execapi.Confinement) error { return execapi.ErrConfineUnsupported }
func (e *Executor) prepareConfinement(_ *ProcessExecutor, _ execapi.ProcessOptions) error {
	return execapi.ErrConfineUnsupported
}
func (e *Executor) bindConfinementEntry() error { return execapi.ErrConfineUnsupported }
