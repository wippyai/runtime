// SPDX-License-Identifier: MPL-2.0

//go:build linux

package affinity

import "golang.org/x/sys/unix"

// pin binds the current OS thread (pid 0) to the given CPU set.
func pin(set Set) error {
	var cs unix.CPUSet
	cs.Zero()
	for _, cpu := range set {
		if cpu >= 0 {
			cs.Set(cpu)
		}
	}
	return unix.SchedSetaffinity(0, &cs)
}
