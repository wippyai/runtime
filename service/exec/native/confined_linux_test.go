// SPDX-License-Identifier: MPL-2.0

package native

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/registry"
	execapi "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/service/exec/native/internal/confinement"
	confinelinux "github.com/wippyai/runtime/service/exec/native/internal/confinement/linux"
	"go.uber.org/zap"
)

func skipConfinementUnavailable(t *testing.T, message string, err error) {
	t.Helper()
	if os.Getenv("WIPPY_REQUIRE_CONFINEMENT") == "1" {
		t.Fatalf("%s: %v", message, err)
	}
	t.Skipf("%s: %v", message, err)
}

func TestConfinedEnvironmentRejectsUnlistedAndPinnedInputs(t *testing.T) {
	policy := &confinement.Environment{
		Allow: []string{"LANG"},
		Set:   map[string]string{"PATH": "/usr/bin"},
	}
	for _, input := range []map[string]string{
		{"LANG": "C", "TOKEN": "secret"},
		{"LANG": "C", "PATH": "/tmp/bin"},
		{"LANG": "C", "HOME": "/host/home"},
		{"LANG": "C", "TMPDIR": "/host/tmp"},
	} {
		process := NewProcessExecutor(zap.NewNop(), WithCmd("/bin/true"), WithEnv(input))
		if err := applyConfinementEnvironment(process, policy, false, true); err == nil {
			t.Fatalf("accepted forbidden environment: %v", input)
		}
		process.releaseFailedStart()
	}
	process := NewProcessExecutor(zap.NewNop(), WithCmd("/bin/true"), WithEnv(map[string]string{"LANG": "C"}))
	defer process.releaseFailedStart()
	if err := applyConfinementEnvironment(process, policy, true, true); err != nil {
		t.Fatal(err)
	}
	if got, want := process.cmd.Env, []string{
		"HOME=" + confinement.PrivateHomePath,
		"LANG=C", "PATH=/usr/bin", "TMPDIR=" + confinement.PrivateTempPath,
	}; !slices.Equal(got, want) {
		t.Fatalf("environment = %v, want %v", got, want)
	}
}

func TestPrivatePathsNeverBecomeHostMounts(t *testing.T) {
	entry := &execapi.Confinement{
		WorkDirRoots: []string{"/workspace"}, Home: "private",
		FS: &execapi.ConfinementFS{
			Read:  []string{"/workspace"},
			Write: []string{"{home}", "{tmp}"},
			Exec:  []string{"/workspace"},
		},
	}
	policy := confinement.ExpandPrivatePolicy(confinement.FromEntry(entry))
	if err := confinement.ValidateEntry(policy); err != nil {
		t.Fatal(err)
	}
	patch := confinement.ExpandPrivatePatch(confinement.FromPatch(&execapi.ConfinementPatch{
		FS: &execapi.ConfinementFSPatch{Write: &[]string{"{tmp}"}},
	}))
	narrowed, err := confinement.Narrow(policy, patch)
	if err != nil {
		t.Fatal(err)
	}
	host, private := splitPrivateFilesystem(narrowed.EffectiveFilesystem())
	mounts, err := confinelinux.PlanBindMounts(host)
	if err != nil {
		t.Fatal(err)
	}
	for _, mount := range mounts {
		if strings.HasPrefix(mount.Source, confinement.PrivateRootPath) {
			t.Fatalf("private path became a host mount: %q", mount.Source)
		}
	}
	if len(private) != 2 || !private[1].Write || private[0].Write {
		t.Fatalf("incorrect narrowed private grants: %+v", private)
	}
}

func TestConfinedPrivateHomeAndTemp(t *testing.T) {
	workspace := t.TempDir()
	helper := filepath.Join(t.TempDir(), "confine-linux")
	target := filepath.Join(workspace, "private-target")
	for output, pkg := range map[string]string{
		helper: "./cmd/confine-linux",
		target: "./internal/confinement/linux/testdata/private",
	} {
		command := exec.CommandContext(t.Context(), "go", "build", "-o", output, pkg)
		command.Env = append(os.Environ(), "CGO_ENABLED=0")
		if result, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build private fixture: %v\n%s", err, result)
		}
	}
	image, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(image)
	oldPath, oldDigest := linuxHelperPath, linuxHelperSHA256
	linuxHelperPath, linuxHelperSHA256 = helper, hex.EncodeToString(digest[:])
	t.Cleanup(func() { linuxHelperPath, linuxHelperSHA256 = oldPath, oldDigest })
	baseline := &execapi.Confinement{
		WorkDirRoots: []string{workspace}, Home: "private",
		FS: &execapi.ConfinementFS{
			Read: []string{workspace}, Write: []string{"{home}", "{tmp}"},
			Exec: []string{workspace},
		},
		Network: "none",
	}
	executor, err := NewExecutorFactory(zap.NewNop()).CreateExecutor(registry.ID{},
		&execapi.NativeExecutorConfig{Confine: baseline})
	if err != nil {
		if errors.Is(err, syscall.EPERM) {
			skipConfinementUnavailable(t, "confinement prerequisites unavailable", err)
		}
		t.Fatal(err)
	}
	defer executor.(interface{ Close() error }).Close()
	process, err := executor.NewProcess(target, execapi.ProcessOptions{WorkDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Start(); err != nil {
		if errors.Is(err, syscall.EPERM) {
			skipConfinementUnavailable(t, "confinement namespace unavailable", err)
		}
		t.Fatal(err)
	}
	result, err := io.ReadAll(process.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Wait(); err != nil {
		t.Fatalf("private target failed: %v, stdout %q", err, result)
	}
	if !strings.Contains(string(result), "private-ok") {
		t.Fatalf("private target did not confirm its environment: %q", result)
	}
}

func TestNativeConfinedLaunchUsesEntryCeiling(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	helper := filepath.Join(t.TempDir(), "confine-linux")
	target := filepath.Join(allowed, "target")
	build := func(output, pkg string) {
		t.Helper()
		command := exec.CommandContext(t.Context(), "go", "build", "-o", output, pkg)
		command.Env = append(os.Environ(), "CGO_ENABLED=0")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build confinement fixture: %v\n%s", err, output)
		}
	}
	build(helper, "./cmd/confine-linux")
	build(target, "./internal/confinement/linux/testdata/target")
	if err := os.WriteFile(filepath.Join(allowed, "visible"), []byte("visible"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "hidden"), []byte("hidden"), 0600); err != nil {
		t.Fatal(err)
	}
	helperBytes, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(helperBytes)
	oldPath, oldDigest := linuxHelperPath, linuxHelperSHA256
	linuxHelperPath, linuxHelperSHA256 = helper, hex.EncodeToString(digest[:])
	t.Cleanup(func() { linuxHelperPath, linuxHelperSHA256 = oldPath, oldDigest })
	baseline := &execapi.Confinement{
		WorkDirRoots: []string{allowed},
		FS: &execapi.ConfinementFS{
			Read: []string{allowed}, Exec: []string{allowed},
		},
		Network: "none",
	}
	factory := NewExecutorFactory(zap.NewNop())
	executor, err := factory.CreateExecutor(registry.ID{}, &execapi.NativeExecutorConfig{Confine: baseline})
	if err != nil {
		if errors.Is(err, syscall.EPERM) {
			skipConfinementUnavailable(t, "confinement prerequisites unavailable", err)
		}
		t.Fatal(err)
	}
	process, err := executor.NewProcess(target, execapi.ProcessOptions{
		WorkDir: allowed,
		Env: map[string]string{
			"VISIBLE": filepath.Join(allowed, "visible"),
			"HIDDEN":  filepath.Join(outside, "hidden"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Start(); err != nil {
		if errors.Is(err, syscall.EPERM) {
			skipConfinementUnavailable(t, "confinement namespace unavailable", err)
		}
		t.Fatal(err)
	}
	if process.(*ProcessExecutor).stdinReader != nil {
		t.Fatal("parent retained the confined child's stdin read endpoint")
	}
	if err := process.Start(); err == nil {
		t.Fatal("second Start replaced the first confined child")
	}
	stdout := process.Stdout()
	defer stdout.Close()
	output, readErr := io.ReadAll(stdout)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err := process.Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := process.(*ProcessExecutor).stdinPipe.Write([]byte("late")); err == nil {
		t.Fatal("stdin writer remained open after confined child exit")
	}
	if !strings.Contains(string(output), "confined") {
		t.Fatalf("target did not complete under confinement: %s", output)
	}
	_, err = executor.NewProcess(target, execapi.ProcessOptions{WorkDir: outside})
	if err == nil {
		t.Fatal("working directory outside entry-owned root was accepted")
	}
}

func TestNativeConfinedNetworkOnlyKeepsFilesystemUnrestricted(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	helper := filepath.Join(t.TempDir(), "confine-linux")
	target := filepath.Join(workspace, "network-target")
	for output, pkg := range map[string]string{
		helper: "./cmd/confine-linux", target: "./internal/confinement/linux/testdata/network",
	} {
		build := exec.CommandContext(t.Context(), "go", "build", "-o", output, pkg)
		build.Env = append(os.Environ(), "CGO_ENABLED=0")
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build network fixture: %v\n%s", err, output)
		}
	}
	contents, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	oldPath, oldDigest := linuxHelperPath, linuxHelperSHA256
	linuxHelperPath, linuxHelperSHA256 = helper, hex.EncodeToString(digest[:])
	t.Cleanup(func() { linuxHelperPath, linuxHelperSHA256 = oldPath, oldDigest })
	executor, err := NewExecutorFactory(zap.NewNop()).CreateExecutor(registry.ID{},
		&execapi.NativeExecutorConfig{Confine: &execapi.Confinement{
			WorkDirRoots: []string{workspace}, Network: "none",
		}})
	if err != nil {
		if errors.Is(err, syscall.EPERM) {
			skipConfinementUnavailable(t, "confinement prerequisites unavailable", err)
		}
		t.Fatal(err)
	}
	process, err := executor.NewProcess(target, execapi.ProcessOptions{
		WorkDir: workspace, Env: map[string]string{"OUTSIDE": outside},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Start(); err != nil {
		if errors.Is(err, syscall.EPERM) {
			skipConfinementUnavailable(t, "confinement namespace unavailable", err)
		}
		t.Fatal(err)
	}
	output, readErr := io.ReadAll(process.Stdout())
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err := process.Wait(); err != nil {
		t.Fatalf("network-only target failed: %v, stdout %q", err, output)
	}
	if !strings.Contains(string(output), "network-only-ok") {
		t.Fatalf("unexpected network-only output %q", output)
	}
}

func TestNativeConfinedLaunchCanFullyNarrowUnrestrictedFilesystem(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	helper := filepath.Join(t.TempDir(), "confine-linux")
	target := filepath.Join(allowed, "target")
	for output, pkg := range map[string]string{
		helper: "./cmd/confine-linux", target: "./internal/confinement/linux/testdata/target",
	} {
		build := exec.CommandContext(t.Context(), "go", "build", "-o", output, pkg)
		build.Env = append(os.Environ(), "CGO_ENABLED=0")
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build narrowing fixture: %v\n%s", err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(allowed, "visible"), []byte("visible"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "hidden"), []byte("hidden"), 0600); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	oldPath, oldDigest := linuxHelperPath, linuxHelperSHA256
	linuxHelperPath, linuxHelperSHA256 = helper, hex.EncodeToString(digest[:])
	t.Cleanup(func() { linuxHelperPath, linuxHelperSHA256 = oldPath, oldDigest })
	executor, err := NewExecutorFactory(zap.NewNop()).CreateExecutor(registry.ID{},
		&execapi.NativeExecutorConfig{Confine: &execapi.Confinement{
			WorkDirRoots: []string{allowed}, Network: "none",
		}})
	if err != nil {
		if errors.Is(err, syscall.EPERM) {
			skipConfinementUnavailable(t, "confinement prerequisites unavailable", err)
		}
		t.Fatal(err)
	}
	read, write, execute := []string{allowed}, []string{}, []string{allowed}
	process, err := executor.NewProcess(target, execapi.ProcessOptions{
		WorkDir: allowed,
		Env: map[string]string{
			"VISIBLE": filepath.Join(allowed, "visible"),
			"HIDDEN":  filepath.Join(outside, "hidden"),
		},
		Confine: &execapi.ConfinementPatch{FS: &execapi.ConfinementFSPatch{
			Read: &read, Write: &write, Exec: &execute,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Start(); err != nil {
		if errors.Is(err, syscall.EPERM) {
			skipConfinementUnavailable(t, "confinement namespace unavailable", err)
		}
		t.Fatal(err)
	}
	output, readErr := io.ReadAll(process.Stdout())
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err := process.Wait(); err != nil {
		t.Fatalf("narrowed target failed: %v, stdout %q", err, output)
	}
	if !strings.Contains(string(output), "confined") {
		t.Fatalf("target did not run under narrowed policy: %q", output)
	}
}

func TestNativeConfinedPrivateHomeWithoutFilesystemPolicy(t *testing.T) {
	workspace := t.TempDir()
	helper := filepath.Join(t.TempDir(), "confine-linux")
	target := filepath.Join(workspace, "home-target")
	for output, pkg := range map[string]string{
		helper: "./cmd/confine-linux", target: "./internal/confinement/linux/testdata/home",
	} {
		build := exec.CommandContext(t.Context(), "go", "build", "-o", output, pkg)
		build.Env = append(os.Environ(), "CGO_ENABLED=0")
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build home fixture: %v\n%s", err, output)
		}
	}
	contents, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	oldPath, oldDigest := linuxHelperPath, linuxHelperSHA256
	linuxHelperPath, linuxHelperSHA256 = helper, hex.EncodeToString(digest[:])
	t.Cleanup(func() { linuxHelperPath, linuxHelperSHA256 = oldPath, oldDigest })
	executor, err := NewExecutorFactory(zap.NewNop()).CreateExecutor(registry.ID{},
		&execapi.NativeExecutorConfig{Confine: &execapi.Confinement{
			WorkDirRoots: []string{workspace}, Home: "private",
		}})
	if err != nil {
		if errors.Is(err, syscall.EPERM) {
			skipConfinementUnavailable(t, "confinement prerequisites unavailable", err)
		}
		t.Fatal(err)
	}
	process, err := executor.NewProcess(target, execapi.ProcessOptions{WorkDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Start(); err != nil {
		if errors.Is(err, syscall.EPERM) {
			skipConfinementUnavailable(t, "confinement namespace unavailable", err)
		}
		t.Fatal(err)
	}
	stdout := bufio.NewReader(process.Stdout())
	line, readErr := stdout.ReadString('\n')
	if readErr != nil {
		t.Fatal(readErr)
	}
	home := strings.TrimSpace(line)
	if home == "" {
		t.Fatal("target did not report private home")
	}
	if _, err := os.Stat(filepath.Join(home, "probe")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private home contents visible from host: %v", err)
	}
	if err := process.(execapi.StdinCloser).CloseStdin(); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(stdout); err != nil {
		t.Fatal(err)
	}
	if err := process.Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private home survived target exit: %v", err)
	}
}

func TestPinnedWriteGrantCannotFallBackToBroadReadGrant(t *testing.T) {
	workspace := t.TempDir()
	safe := filepath.Join(workspace, "safe")
	if err := os.Mkdir(safe, 0700); err != nil {
		t.Fatal(err)
	}
	read, err := confinelinux.BindDeclaredDirectory(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	write, err := confinelinux.BindDeclaredDirectory(safe)
	if err != nil {
		t.Fatal(err)
	}
	defer write.Close()
	binding := &linuxEntryBinding{
		read:  map[string]*confinelinux.BoundDirectory{workspace: read},
		write: map[string]*confinelinux.BoundDirectory{safe: write},
	}
	if err := os.Rename(safe, filepath.Join(workspace, "old-safe")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(safe, 0700); err != nil {
		t.Fatal(err)
	}
	if grant, _, err := binding.openGrant(safe, true, true, false); err == nil {
		grant.Close()
		t.Fatal("replacement inherited pinned write authority through a read grant")
	}
}

func TestNativeConfinedDynamicShellThroughMergedUsrAliases(t *testing.T) {
	for _, path := range []string{"/bin/sh", "/lib", "/lib64"} {
		if _, err := os.Lstat(path); err != nil {
			t.Skipf("dynamic shell layout unavailable: %v", err)
		}
	}
	helper := filepath.Join(t.TempDir(), "confine-linux")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", helper, "./cmd/confine-linux")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, output)
	}
	contents, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(contents)
	oldPath, oldDigest := linuxHelperPath, linuxHelperSHA256
	linuxHelperPath, linuxHelperSHA256 = helper, hex.EncodeToString(hash[:])
	t.Cleanup(func() { linuxHelperPath, linuxHelperSHA256 = oldPath, oldDigest })
	workspace := t.TempDir()
	baseline := &execapi.Confinement{
		WorkDirRoots: []string{workspace},
		FS: &execapi.ConfinementFS{
			Read: []string{workspace, "/bin", "/lib", "/lib64"},
			Exec: []string{"/bin"},
		},
		Network: "none",
	}
	executor, err := NewExecutorFactory(zap.NewNop()).CreateExecutor(registry.ID{},
		&execapi.NativeExecutorConfig{Confine: baseline})
	if err != nil {
		t.Fatal(err)
	}
	process, err := executor.NewProcess("/bin/sh -c 'printf dynamic-ok'", execapi.ProcessOptions{WorkDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Start(); err != nil {
		if errors.Is(err, syscall.EPERM) {
			skipConfinementUnavailable(t, "required namespace unavailable", err)
		}
		if strings.Contains(err.Error(), "bind pinned directory") && strings.Contains(err.Error(), "invalid argument") {
			skipConfinementUnavailable(t, "host user namespace cannot bind system-root libraries", err)
		}
		t.Fatal(err)
	}
	stdout := process.Stdout()
	defer stdout.Close()
	output, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Wait(); err != nil {
		t.Fatal(err)
	}
	if string(output) != "dynamic-ok" {
		t.Fatalf("dynamic shell did not run: %q", output)
	}
}

func TestNativeConfinedTreeDiesWithRoot(t *testing.T) {
	workspace := t.TempDir()
	helper := filepath.Join(t.TempDir(), "confine-linux")
	target := filepath.Join(workspace, "tree-target")
	for output, pkg := range map[string]string{
		helper: "./cmd/confine-linux", target: "./internal/confinement/linux/testdata/tree",
	} {
		build := exec.CommandContext(t.Context(), "go", "build", "-o", output, pkg)
		build.Env = append(os.Environ(), "CGO_ENABLED=0")
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build tree fixture: %v\n%s", err, output)
		}
	}
	contents, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(contents)
	oldPath, oldDigest := linuxHelperPath, linuxHelperSHA256
	linuxHelperPath, linuxHelperSHA256 = helper, hex.EncodeToString(hash[:])
	t.Cleanup(func() { linuxHelperPath, linuxHelperSHA256 = oldPath, oldDigest })
	baseline := &execapi.Confinement{
		WorkDirRoots: []string{workspace},
		FS: &execapi.ConfinementFS{
			Read: []string{workspace}, Write: []string{workspace}, Exec: []string{workspace},
		},
		Network: "none", Limits: &execapi.ConfinementLimits{WallSec: 1},
		Tree: &execapi.ConfinementTree{KillOnOwnerExit: true},
	}
	executor, err := NewExecutorFactory(zap.NewNop()).CreateExecutor(registry.ID{},
		&execapi.NativeExecutorConfig{Confine: baseline})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"exit", "wait", "wall"} {
		t.Run(mode, func(t *testing.T) {
			marker := filepath.Join(workspace, "heartbeat-"+mode)
			targetMode := mode
			if mode == "wall" {
				targetMode = "wait"
			}
			process, err := executor.NewProcess(target+" "+targetMode, execapi.ProcessOptions{
				WorkDir: workspace, Env: map[string]string{"HEARTBEAT": marker},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := process.Start(); err != nil {
				if errors.Is(err, syscall.EPERM) {
					skipConfinementUnavailable(t, "required namespace unavailable", err)
				}
				t.Fatal(err)
			}
			stderr := process.Stderr()
			defer stderr.Close()
			stderrOutput := make(chan []byte, 1)
			go func() {
				output, _ := io.ReadAll(stderr)
				stderrOutput <- output
			}()
			waitResult := make(chan error, 1)
			go func() { waitResult <- process.Wait() }()
			deadline := time.Now().Add(2 * time.Second)
			for {
				if _, err := os.Stat(marker); err == nil {
					break
				}
				if time.Now().After(deadline) {
					_, rootErr := os.Stat(marker + ".root")
					t.Logf("root marker: %v", rootErr)
					select {
					case waitErr := <-waitResult:
						t.Logf("root exit: %v", waitErr)
					default:
					}
					select {
					case output := <-stderrOutput:
						t.Logf("tree target stderr: %s", output)
					default:
					}
					process.(*ProcessExecutor).Stop()
					t.Fatal("daemonized child never started")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if mode == "wait" {
				process.(*ProcessExecutor).Stop()
			}
			waitErr := <-waitResult
			if mode == "exit" && waitErr != nil {
				t.Fatal(waitErr)
			}
			if mode == "wall" {
				var exit *ExitError
				if !errors.As(waitErr, &exit) || exit.Code != 137 {
					t.Fatalf("wall timeout result = %v, want exit 137", waitErr)
				}
			}
			before, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(160 * time.Millisecond)
			after, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatalf("daemonized child survived root %s: %q -> %q", mode, before, after)
			}
		})
	}
}

func TestNativeConfinedIdentityAndDefaultSignalSemantics(t *testing.T) {
	workspace := t.TempDir()
	helper := filepath.Join(t.TempDir(), "confine-linux")
	target := filepath.Join(workspace, "signal-target")
	for output, pkg := range map[string]string{
		helper: "./cmd/confine-linux", target: "./internal/confinement/linux/testdata/signal",
	} {
		build := exec.CommandContext(t.Context(), "go", "build", "-o", output, pkg)
		build.Env = append(os.Environ(), "CGO_ENABLED=0")
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build signal fixture: %v\n%s", err, output)
		}
	}
	contents, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(contents)
	oldPath, oldDigest := linuxHelperPath, linuxHelperSHA256
	linuxHelperPath, linuxHelperSHA256 = helper, hex.EncodeToString(hash[:])
	t.Cleanup(func() { linuxHelperPath, linuxHelperSHA256 = oldPath, oldDigest })
	executor, err := NewExecutorFactory(zap.NewNop()).CreateExecutor(registry.ID{},
		&execapi.NativeExecutorConfig{Confine: &execapi.Confinement{
			WorkDirRoots: []string{workspace},
			FS:           &execapi.ConfinementFS{Read: []string{workspace}, Exec: []string{workspace}},
			Network:      "none", Tree: &execapi.ConfinementTree{KillOnOwnerExit: true},
		}})
	if err != nil {
		t.Fatal(err)
	}
	process, err := executor.NewProcess(target, execapi.ProcessOptions{WorkDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Start(); err != nil {
		if errors.Is(err, syscall.EPERM) {
			skipConfinementUnavailable(t, "required namespace unavailable", err)
		}
		t.Fatal(err)
	}
	line, err := bufio.NewReader(process.Stdout()).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	reported, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := process.(execapi.ProcessIdentity).Pid()
	if err != nil || pid <= 0 || reported == 1 {
		t.Fatalf("Pid() = %d, %v; target namespace pid %d", pid, err, reported)
	}
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(status), fmt.Sprintf("\t%d\n", reported)) {
		t.Fatalf("Pid() does not identify target namespace pid %d: %s", reported, status)
	}
	if err := process.Signal(int(syscall.SIGTERM)); err != nil {
		t.Fatal(err)
	}
	var exit *ExitError
	waitErr := process.Wait()
	if !errors.As(waitErr, &exit) || exit.Code != 143 {
		t.Fatalf("SIGTERM result = %v, want exit 143", waitErr)
	}
	classified := execapi.ClassifyExit(waitErr)
	if classified.Code != 143 || classified.Signal != int(syscall.SIGTERM) || classified.Err != nil {
		t.Fatalf("classified SIGTERM = %+v", classified)
	}
}

func TestNativeConfinedPTYResize(t *testing.T) {
	workspace := t.TempDir()
	helper := filepath.Join(t.TempDir(), "confine-linux")
	target := filepath.Join(workspace, "pty-target")
	for output, pkg := range map[string]string{
		helper: "./cmd/confine-linux", target: "./internal/confinement/linux/testdata/pty",
	} {
		build := exec.CommandContext(t.Context(), "go", "build", "-o", output, pkg)
		build.Env = append(os.Environ(), "CGO_ENABLED=0")
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build PTY fixture: %v\n%s", err, output)
		}
	}
	contents, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(contents)
	oldPath, oldDigest := linuxHelperPath, linuxHelperSHA256
	linuxHelperPath, linuxHelperSHA256 = helper, hex.EncodeToString(hash[:])
	t.Cleanup(func() { linuxHelperPath, linuxHelperSHA256 = oldPath, oldDigest })
	baseline := &execapi.Confinement{
		WorkDirRoots: []string{workspace},
		FS: &execapi.ConfinementFS{
			Read: []string{workspace}, Exec: []string{workspace},
		},
		Network: "none", Tree: &execapi.ConfinementTree{KillOnOwnerExit: true},
	}
	executor, err := NewExecutorFactory(zap.NewNop()).CreateExecutor(registry.ID{},
		&execapi.NativeExecutorConfig{Confine: baseline})
	if err != nil {
		t.Fatal(err)
	}
	process, err := executor.NewProcess(target, execapi.ProcessOptions{
		WorkDir: workspace, PTY: &execapi.PTYOptions{Width: 80, Height: 24, Term: "xterm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Start(); err != nil {
		if errors.Is(err, syscall.EPERM) {
			skipConfinementUnavailable(t, "required namespace unavailable", err)
		}
		t.Fatal(err)
	}
	terminal := process.(execapi.PTYProcess)
	output := process.Stdout()
	defer output.Close()
	reader := bufio.NewReader(output)
	readUntil := func(want string) {
		t.Helper()
		lines := make(chan string, 1)
		go func() {
			for {
				line, readErr := reader.ReadString('\n')
				if readErr != nil || strings.TrimSpace(line) == want {
					lines <- strings.TrimSpace(line)
					return
				}
			}
		}()
		select {
		case line := <-lines:
			if line != want {
				t.Fatalf("PTY size: got %q, want %q", line, want)
			}
		case <-time.After(3 * time.Second):
			process.(*ptyProcess).Stop()
			t.Fatalf("timed out waiting for PTY size %q", want)
		}
	}
	readUntil("24 80")
	if err := terminal.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	if err := process.WriteStdin([]byte("continue\n")); err != nil {
		t.Fatal(err)
	}
	readUntil("30 100")
	if err := process.Wait(); err != nil {
		t.Fatal(err)
	}
}
