// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/wippyai/runtime/service/exec/native/internal/confinement"
)

var ErrUnrestrictedMountClass = errors.New("unrestricted filesystem class needs a separate mount plan")

// BindMount is one host subtree in the child's private filesystem view.
// Landlock still mediates read/write/exec separately after these mounts are
// installed; mount flags alone cannot express those independent rights.
type BindMount struct {
	Source   string
	ReadOnly bool
	NoExec   bool
}

// PlanBindMounts orders broad mounts before narrow overlays. A read-only,
// no-exec parent can therefore contain a writable or executable child grant.
// Every path must already be expanded and lexically validated. This function
// only describes mount precedence and flags; the launch path separately binds
// each declaration to a pinned object before applying the plan.
func PlanBindMounts(fs confinement.Filesystem) ([]BindMount, error) {
	if fs.Read.Unrestricted || fs.Write.Unrestricted || fs.Exec.Unrestricted {
		return nil, ErrUnrestrictedMountClass
	}
	paths := make(map[string]struct{})
	for _, class := range []confinement.Access{fs.Read, fs.Write, fs.Exec} {
		for _, path := range class.Paths {
			if !cleanAbsolute(path) {
				return nil, fmt.Errorf("%w: filesystem grant must be clean and absolute", confinement.ErrInvalid)
			}
			paths[path] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	slices.SortFunc(ordered, func(a, b string) int {
		if depthA, depthB := strings.Count(a, "/"), strings.Count(b, "/"); depthA != depthB {
			return depthA - depthB
		}
		return strings.Compare(a, b)
	})

	mounts := make([]BindMount, 0, len(ordered))
	for _, path := range ordered {
		mounts = append(mounts, BindMount{
			Source:   path,
			ReadOnly: !fs.Write.Covers(path),
			NoExec:   !fs.Exec.Covers(path),
		})
	}
	return mounts, nil
}
