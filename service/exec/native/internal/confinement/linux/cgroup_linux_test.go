// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wippyai/runtime/service/exec/native/internal/confinement"
)

func TestCgroupLimitsAndKill(t *testing.T) {
	if err := ProbeCgroupDelegation(confinement.Limits{MemoryMiB: 64, PIDs: 4}); err != nil {
		if os.Getenv("WIPPY_REQUIRE_CONFINEMENT_CGROUP") == "1" {
			t.Fatalf("required cgroup delegation unavailable: %v", err)
		}
		t.Skipf("cgroup delegation unavailable: %v", err)
	}
	group, err := NewCgroup(confinement.Limits{MemoryMiB: 64, PIDs: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = group.Kill()
		if err := group.Remove(); err != nil {
			t.Error(err)
		}
	}()
	for name, want := range map[string]string{"memory.max": "67108864", "pids.max": "4", "memory.oom.group": "1"} {
		value, err := os.ReadFile(filepath.Join(group.path, name))
		if err != nil || strings.TrimSpace(string(value)) != want {
			t.Fatalf("%s = %q, %v; want %q", name, value, err, want)
		}
	}
	child := exec.CommandContext(t.Context(), "sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	if err := group.Add(child.Process.Pid); err != nil {
		_ = child.Process.Kill()
		_ = child.Wait()
		t.Fatal(err)
	}
	if err := group.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("cgroup.kill did not terminate the member")
	}
}

func TestCgroupKillAfterRemoveIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "removed")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	group := &Cgroup{path: path}
	if err := group.Remove(); err != nil {
		t.Fatal(err)
	}
	if err := group.Kill(); err != nil {
		t.Fatalf("Kill() after successful Remove() = %v, want nil", err)
	}
}
