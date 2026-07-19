// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"path/filepath"
	"testing"

	"github.com/wippyai/runtime/boot/deps/lock"
)

// A locally replaced module cannot be resolved from the hub, so the rebuilt
// lock never contains its row. The update must carry the old row forward —
// dropping it detaches the replacement from the module graph and every
// resource the module declared fails to link on the next run.
func TestPreserveReplacedModuleRows_CarriesLocalRootModule(t *testing.T) {
	dir := t.TempDir()

	oldLock, err := lock.New(filepath.Join(dir, "old", "wippy.lock"))
	if err != nil {
		t.Fatal(err)
	}
	oldLock.SetModule(lock.Module{Name: "acme/starter-harness", Version: "0.1.0", Root: true})
	oldLock.SetModule(lock.Module{Name: "kickside/contract", Version: "0.1.16", Hash: "sha256:x"})
	oldLock.SetReplacement(lock.Replacement{From: "acme/starter-harness", To: "."})

	newLock, err := convertResolvedToLock(filepath.Join(dir, "new", "wippy.lock"), nil, ".wippy", "../src")
	if err != nil {
		t.Fatal(err)
	}
	preserveReplacements(newLock, oldLock.GetTrackedReplacements())
	preserveReplacedModuleRows(newLock, oldLock)

	mod, ok := newLock.GetModule("acme/starter-harness")
	if !ok {
		t.Fatal("replaced module row must survive the lock rebuild")
	}
	if !mod.Root || mod.Version != "0.1.0" {
		t.Fatalf("replaced module row must keep root flag and version, got %+v", mod)
	}
	// Hub-resolvable modules are never carried: the rebuild owns them.
	if _, ok := newLock.GetModule("kickside/contract"); ok {
		t.Fatal("non-replaced modules must come from resolution, not the old lock")
	}
}

// A replacement whose module row was never in the old lock has nothing to
// preserve; the update must not invent one.
func TestPreserveReplacedModuleRows_NoRowNoInvention(t *testing.T) {
	dir := t.TempDir()

	oldLock, err := lock.New(filepath.Join(dir, "old", "wippy.lock"))
	if err != nil {
		t.Fatal(err)
	}
	oldLock.SetReplacement(lock.Replacement{From: "acme/ghost", To: "./ghost"})

	newLock, err := convertResolvedToLock(filepath.Join(dir, "new", "wippy.lock"), nil, ".wippy", "../src")
	if err != nil {
		t.Fatal(err)
	}
	preserveReplacedModuleRows(newLock, oldLock)

	if _, ok := newLock.GetModule("acme/ghost"); ok {
		t.Fatal("must not invent a module row for a rowless replacement")
	}
}

// A replaced module that the resolution DID produce (e.g. the replacement was
// added while the module is still a hub dependency of something) keeps the
// resolved row; the old row must not clobber it.
func TestPreserveReplacedModuleRows_ResolvedRowWins(t *testing.T) {
	dir := t.TempDir()

	oldLock, err := lock.New(filepath.Join(dir, "old", "wippy.lock"))
	if err != nil {
		t.Fatal(err)
	}
	oldLock.SetModule(lock.Module{Name: "acme/tools", Version: "0.0.9"})
	oldLock.SetReplacement(lock.Replacement{From: "acme/tools", To: "../tools"})

	newLock, err := convertResolvedToLock(filepath.Join(dir, "new", "wippy.lock"), nil, ".wippy", "../src")
	if err != nil {
		t.Fatal(err)
	}
	newLock.SetModule(lock.Module{Name: "acme/tools", Version: "0.1.0", Hash: "sha256:fresh"})
	preserveReplacedModuleRows(newLock, oldLock)

	mod, _ := newLock.GetModule("acme/tools")
	if mod.Version != "0.1.0" {
		t.Fatalf("resolved row must win over the carried row, got %+v", mod)
	}
}
