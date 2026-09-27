// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wippyai/runtime/service/exec/native/internal/confinement"
	"golang.org/x/sys/unix"
)

const cgroupMount = "/sys/fs/cgroup"

// Cgroup owns one delegated cgroup v2 leaf for a confined launch. It is never
// used as a substitute for filesystem, network or IPC confinement.
type Cgroup struct {
	path    string
	mu      sync.Mutex
	removed bool
}

func delegatedCgroupRoot() (string, error) {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "0::/") {
			continue
		}
		rel := strings.TrimPrefix(line, "0::/")
		if rel == "" {
			return "", errors.New("runtime is not in a delegated cgroup runtime leaf")
		}
		if filepath.Clean(rel) != rel || rel == ".." || strings.HasPrefix(rel, "../") {
			return "", errors.New("invalid cgroup v2 path")
		}
		// systemd's DelegateSubgroup=runtime (or an equivalent service
		// manager layout) places the runtime in a leaf and leaves its parent
		// empty for controller delegation and sibling per-launch groups.
		if filepath.Base(rel) != "runtime" || filepath.Dir(rel) == "." {
			return "", errors.New("confinement needs an empty delegated parent with a runtime leaf")
		}
		root := filepath.Join(cgroupMount, filepath.Dir(rel))
		var stat unix.Statfs_t
		if err := unix.Statfs(root, &stat); err != nil || stat.Type != unix.CGROUP2_SUPER_MAGIC {
			return "", errors.New("delegated root is not cgroup v2")
		}
		return root, nil
	}
	return "", errors.New("no unified cgroup v2 membership")
}

func ProbeCgroupDelegation(limits confinement.Limits) error {
	root, err := prepareDelegatedCgroupRoot(limits)
	if err != nil {
		return err
	}
	probe, err := os.MkdirTemp(root, "wippy-confine-probe-")
	if err != nil {
		return fmt.Errorf("create delegated cgroup leaf: %w", err)
	}
	defer os.Remove(probe)
	files := []string{"cgroup.procs", "cgroup.kill"}
	if limits.MemoryMiB > 0 {
		files = append(files, "memory.max", "memory.oom.group")
	}
	if limits.PIDs > 0 {
		files = append(files, "pids.max")
	}
	for _, name := range files {
		if _, err := os.Stat(filepath.Join(probe, name)); err != nil {
			return fmt.Errorf("missing delegated cgroup %s: %w", name, err)
		}
	}
	// A readable file is not a writable controller. Write the requested
	// ceilings on this disposable leaf before claiming host support.
	if limits.MemoryMiB > 0 {
		if err := os.WriteFile(filepath.Join(probe, "memory.max"),
			[]byte(strconv.FormatInt(limits.MemoryMiB*1024*1024, 10)), 0); err != nil {
			return fmt.Errorf("write delegated memory limit: %w", err)
		}
	}
	if limits.PIDs > 0 {
		if err := os.WriteFile(filepath.Join(probe, "pids.max"),
			[]byte(strconv.FormatInt(limits.PIDs, 10)), 0); err != nil {
			return fmt.Errorf("write delegated pids limit: %w", err)
		}
	}
	return nil
}

func prepareDelegatedCgroupRoot(limits confinement.Limits) (string, error) {
	root, err := delegatedCgroupRoot()
	if err != nil {
		return "", err
	}
	procs, err := os.ReadFile(filepath.Join(root, "cgroup.procs"))
	if err != nil || len(strings.TrimSpace(string(procs))) != 0 {
		return "", errors.New("delegated cgroup parent is not empty")
	}
	available, err := os.ReadFile(filepath.Join(root, "cgroup.controllers"))
	if err != nil {
		return "", fmt.Errorf("read delegated controllers: %w", err)
	}
	enabled, err := os.ReadFile(filepath.Join(root, "cgroup.subtree_control"))
	if err != nil {
		return "", fmt.Errorf("read delegated subtree controls: %w", err)
	}
	for _, controller := range []struct {
		name      string
		requested bool
	}{
		{"memory", limits.MemoryMiB > 0},
		{"pids", limits.PIDs > 0},
	} {
		if !controller.requested {
			continue
		}
		if !slices.Contains(strings.Fields(string(available)), controller.name) {
			return "", fmt.Errorf("delegated %s controller unavailable", controller.name)
		}
		if !slices.Contains(strings.Fields(string(enabled)), controller.name) {
			if err := os.WriteFile(filepath.Join(root, "cgroup.subtree_control"),
				[]byte("+"+controller.name), 0); err != nil {
				return "", fmt.Errorf("enable delegated %s controller: %w", controller.name, err)
			}
		}
	}
	return root, nil
}

func NewCgroup(limits confinement.Limits) (*Cgroup, error) {
	if limits.MemoryMiB <= 0 && limits.PIDs <= 0 {
		return nil, nil
	}
	root, err := prepareDelegatedCgroupRoot(limits)
	if err != nil {
		return nil, err
	}
	path, err := os.MkdirTemp(root, "wippy-confine-")
	if err != nil {
		return nil, fmt.Errorf("create delegated cgroup leaf: %w", err)
	}
	group := &Cgroup{path: path}
	if limits.MemoryMiB > 0 {
		if err := group.write("memory.max", strconv.FormatInt(limits.MemoryMiB*1024*1024, 10)); err != nil {
			_ = group.Remove()
			return nil, err
		}
		if err := group.write("memory.oom.group", "1"); err != nil {
			_ = group.Remove()
			return nil, err
		}
	}
	if limits.PIDs > 0 {
		if err := group.write("pids.max", strconv.FormatInt(limits.PIDs, 10)); err != nil {
			_ = group.Remove()
			return nil, err
		}
	}
	return group, nil
}

func (g *Cgroup) Add(pid int) error {
	if g == nil || pid <= 0 {
		return errors.New("invalid cgroup child")
	}
	if err := g.write("cgroup.procs", strconv.Itoa(pid)); err != nil {
		return err
	}
	procs, err := os.ReadFile(filepath.Join(g.path, "cgroup.procs"))
	if err != nil {
		return fmt.Errorf("verify cgroup membership: %w", err)
	}
	if !slices.Contains(strings.Fields(string(procs)), strconv.Itoa(pid)) {
		return errors.New("confined child did not enter its cgroup")
	}
	return nil
}

func (g *Cgroup) Kill() error {
	if g == nil {
		return nil
	}
	return g.write("cgroup.kill", "1")
}

func (g *Cgroup) Remove() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.removed {
		return nil
	}
	var result error
	for attempt := 0; attempt < 100; attempt++ {
		result = os.Remove(g.path)
		if result == nil || errors.Is(result, os.ErrNotExist) {
			g.removed = true
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return result
}

func (g *Cgroup) write(name, value string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.removed {
		return os.ErrClosed
	}
	path := filepath.Join(g.path, name)
	if err := os.WriteFile(path, []byte(value), 0); err != nil {
		return fmt.Errorf("write cgroup %s: %w", name, err)
	}
	return nil
}
