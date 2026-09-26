// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestSeparateHelperExecsOnlyAfterConfinement(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	root := t.TempDir()
	helper := filepath.Join(t.TempDir(), "confine-linux")
	target := filepath.Join(allowed, "target")
	build := func(output, pkg string) {
		t.Helper()
		command := exec.CommandContext(t.Context(), "go", "build", "-o", output, pkg)
		command.Env = append(os.Environ(), "CGO_ENABLED=0")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build fixture: %v\n%s", err, output)
		}
	}
	build(helper, "../../../cmd/confine-linux")
	build(target, "./testdata/target")
	if err := os.WriteFile(filepath.Join(allowed, "visible"), []byte("visible"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "hidden"), []byte("hidden"), 0600); err != nil {
		t.Fatal(err)
	}
	grant, err := BindDirectory(allowed)
	if err != nil {
		t.Fatal(err)
	}
	defer grant.Close()
	workdir, err := BindDirectory(allowed)
	if err != nil {
		t.Fatal(err)
	}
	defer workdir.Close()
	helperBytes, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	helperHash := sha256.Sum256(helperBytes)
	helperFile, err := OpenVerifiedHelper(helper, hex.EncodeToString(helperHash[:]))
	if err != nil {
		t.Fatal(err)
	}
	defer helperFile.Close()
	policy := HelperPolicy{
		Root: root,
		Grants: []HelperGrant{{
			Source: allowed, Target: "/grant", Read: true, Exec: true, ReadOnly: true,
		}},
		WorkDir: "/grant",
		Path:    "/grant/target", Argv: []string{"/grant/target"},
		Env: []string{
			"VISIBLE=/grant/visible",
			"HIDDEN=" + filepath.Join(outside, "hidden"),
		},
	}
	launch, err := PrepareLaunch(helperFile, policy, []*os.File{grant}, workdir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer launch.Close()
	var output bytes.Buffer
	launch.Command.Stdout = &output
	launch.Command.Stderr = &output
	if err := launch.Start(func(command *exec.Cmd) error { return command.Start() }, nil); errors.Is(err, syscall.EPERM) {
		if os.Getenv("WIPPY_REQUIRE_CONFINEMENT") == "1" {
			t.Fatalf("required namespaces unavailable: %v", err)
		}
		t.Skipf("required namespaces unavailable: %v", err)
	} else if err != nil {
		t.Fatal(err)
	}
	err = launch.Command.Wait()
	if err != nil || !bytes.Contains(output.Bytes(), []byte("confined")) {
		t.Fatalf("confined target failed: %v\n%s", err, output.String())
	}
}
