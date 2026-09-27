// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"strings"
	"testing"
)

func TestValidateKernelMountTopology(t *testing.T) {
	valid := strings.Join([]string{
		"20 1 0:20 / /proc rw - proc proc rw",
		"21 20 0:20 /sys /proc/sys ro - proc proc rw",
		"22 1 0:22 / /sys/fs/cgroup rw - cgroup2 cgroup rw",
		"23 1 0:23 / /workspace rw - ext4 disk rw",
	}, "\n")
	if err := validateKernelMountTopology(strings.NewReader(valid)); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		"20 1 0:20 / /host-proc rw - proc proc rw",
		"20 1 0:20 / /proc rw - proc proc rw\n22 1 0:22 / /escape rw - cgroup2 cgroup rw",
		"22 1 0:22 / /sys/fs/cgroup rw - cgroup2 cgroup rw",
		"not mountinfo",
	} {
		if err := validateKernelMountTopology(strings.NewReader(invalid)); err == nil {
			t.Fatalf("accepted unsafe mount topology %q", invalid)
		}
	}
}

func TestUnescapeMountInfoPath(t *testing.T) {
	if got, want := unescapeMountInfoPath(`/path\040with\134slash`), "/path with\\slash"; got != want {
		t.Fatalf("unescapeMountInfoPath = %q, want %q", got, want)
	}
}
