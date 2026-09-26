// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"errors"
	"fmt"
	"path/filepath"
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
// Every path must already be expanded and securely bound before this plan is
// applied; this function never authorizes a raw caller-controlled path.
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
			ReadOnly: !coveredByGrant(path, fs.Write.Paths),
			NoExec:   !coveredByGrant(path, fs.Exec.Paths),
		})
	}
	return mounts, nil
}

func coveredByGrant(path string, grants []string) bool {
	for _, grant := range grants {
		rel, err := filepath.Rel(grant, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return true
		}
	}
	return false
}
