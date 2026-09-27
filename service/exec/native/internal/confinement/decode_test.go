// SPDX-License-Identifier: MPL-2.0

package confinement

import (
	"testing"

	execapi "github.com/wippyai/runtime/api/service/exec"
)

func TestDecodePolicyDoesNotAliasEntry(t *testing.T) {
	entry := &execapi.Confinement{
		WorkDirRoots: []string{nativePath("/workspace")},
		FS:           &execapi.ConfinementFS{Read: []string{nativePath("/workspace")}},
		Env:          &execapi.ConfinementEnvironment{Allow: []string{"LANG"}, Set: map[string]string{"PATH": "/bin"}},
		Network:      "none",
	}
	policy := FromEntry(entry)
	policy.WorkDirRoots[0] = nativePath("/other")
	policy.FS.Read.Paths[0] = nativePath("/other")
	policy.Env.Allow[0] = "OTHER"
	policy.Env.Set["PATH"] = "/other"
	if entry.WorkDirRoots[0] != nativePath("/workspace") ||
		entry.FS.Read[0] != nativePath("/workspace") ||
		entry.Env.Allow[0] != "LANG" || entry.Env.Set["PATH"] != "/bin" {
		t.Fatal("decoded policy aliases the registry entry")
	}
}

func TestDecodePatchPreservesPresentEmptyList(t *testing.T) {
	empty := []string{}
	input := &execapi.ConfinementPatch{FS: &execapi.ConfinementFSPatch{Read: &empty}}
	patch := FromPatch(input)
	if patch.FS == nil || patch.FS.Read == nil || len(*patch.FS.Read) != 0 || patch.FS.Write != nil {
		t.Fatalf("wrong patch presence: %#v", patch.FS)
	}
	*patch.FS.Read = append(*patch.FS.Read, nativePath("/elsewhere"))
	if len(empty) != 0 {
		t.Fatal("decoded patch aliases caller input")
	}
}
