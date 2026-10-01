// SPDX-License-Identifier: MPL-2.0

package directory

import "io/fs"

// Mark policy refusals separately from absent files. The Lua exists adapter
// preserves these errors without changing contained-policy existence checks.
type linkPolicyRefusal struct{ reason error }

func (e *linkPolicyRefusal) Error() string           { return "owner_safe: " + e.reason.Error() }
func (e *linkPolicyRefusal) Unwrap() error           { return fs.ErrPermission }
func (e *linkPolicyRefusal) LinkPolicyRefusal() bool { return true }
