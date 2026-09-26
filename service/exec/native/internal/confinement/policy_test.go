// SPDX-License-Identifier: MPL-2.0

package confinement

import (
	"errors"
	"reflect"
	"testing"
)

func paths(values ...string) *[]string { return &values }

func number(value int64) *int64 { return &value }

func basePolicy() Policy {
	return Policy{
		WorkDirRoots: []string{"/srv/ws/demo"},
		FS: &Filesystem{
			Read:  Access{Paths: []string{"/srv/ws/demo", "/usr"}},
			Write: Access{Paths: []string{"/srv/ws/demo"}},
			Exec:  Access{Paths: []string{"/usr/bin"}},
		},
		Env: &Environment{
			Allow: []string{"LANG", "TERM"},
			Set:   map[string]string{"PATH": "/usr/bin"},
		},
		NetworkNone:     true,
		Limits:          Limits{MemoryMiB: 4096, PIDs: 256, WallSec: 300},
		KillOnOwnerExit: true,
	}
}

func TestValidateEntry(t *testing.T) {
	if err := ValidateEntry(basePolicy()); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Policy){
		func(p *Policy) { p.WorkDirRoots = nil },
		func(p *Policy) {
			p.WorkDirRoots = []string{"/"}
			p.FS = nil
			p.Env = nil
			p.HomePrivate = false
			p.NetworkNone = false
			p.Limits = Limits{}
			p.KillOnOwnerExit = false
		},
		func(p *Policy) { p.WorkDirRoots = []string{"/srv/ws/../other"} },
		func(p *Policy) { p.FS.Exec = Access{Paths: []string{"relative"}} },
		func(p *Policy) { p.FS.Exec.Unrestricted = true },
		func(p *Policy) { p.Limits.PIDs = -1 },
	} {
		policy := basePolicy()
		change(&policy)
		if err := ValidateEntry(policy); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted malformed entry %#v: %v", policy, err)
		}
	}
}

func TestWriteGrantImpliesRead(t *testing.T) {
	base := basePolicy()
	base.FS.Write.Paths = []string{"/srv/ws/demo", "/tmp/private"}
	if err := ValidateEntry(base); err != nil {
		t.Fatalf("rejected a write grant that implicitly permits reading: %v", err)
	}
	if _, err := Narrow(base, Patch{}); err != nil {
		t.Fatalf("identity patch rejected implicit read: %v", err)
	}
	if _, err := Narrow(base, Patch{FS: &PathsPatch{Read: paths("/srv/ws/demo")}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("explicit read patch excluded an inherited write grant: %v", err)
	}
}

func TestNarrowIdentityAndNoMutation(t *testing.T) {
	base := basePolicy()
	before := clonePolicy(base)
	got, err := Narrow(base, Patch{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(base, before) {
		t.Fatal("Narrow mutated the registry baseline")
	}
	if !reflect.DeepEqual(got, before) {
		t.Fatalf("identity patch changed policy: %#v", got)
	}
	got.Env.Set["PATH"] = "/attacker"
	got.WorkDirRoots[0] = "/"
	if !reflect.DeepEqual(base, before) {
		t.Fatal("returned policy aliases the registry baseline")
	}
}

func TestBoundWorkDirMustStayInEntryRootAndReadGrant(t *testing.T) {
	policy := basePolicy()
	for _, path := range []string{
		"/", "/srv/ws/other", "/srv/ws/demo-other", "/srv/ws/demo/../other",
		"relative",
	} {
		if policy.AllowsBoundWorkDir(path) {
			t.Fatalf("accepted working directory %q", path)
		}
	}
	if !policy.AllowsBoundWorkDir("/srv/ws/demo/pkg") {
		t.Fatal("rejected a directory inside the approved root and read grant")
	}
	policy.FS.Read.Paths = []string{"/usr"}
	policy.FS.Write.Paths = nil
	if policy.AllowsBoundWorkDir("/srv/ws/demo/pkg") {
		t.Fatal("accepted a working directory excluded by fs.read")
	}
}

func TestNarrowFilesystem(t *testing.T) {
	base := basePolicy()
	got, err := Narrow(base, Patch{FS: &PathsPatch{
		Read:  paths("/srv/ws/demo/pkg", "/usr"),
		Write: paths("/srv/ws/demo/pkg"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.FS.Write.Paths, []string{"/srv/ws/demo/pkg"}) {
		t.Fatalf("wrong write grants: %#v", got.FS.Write.Paths)
	}
	for _, patch := range []Patch{
		{FS: &PathsPatch{Read: paths("/srv/ws/demo/pkg")}},
		{FS: &PathsPatch{Write: paths("/srv/ws/other")}},
		{FS: &PathsPatch{Exec: paths("/usr/bin-extra")}},
		{FS: &PathsPatch{Write: paths("/")}},
		{FS: &PathsPatch{Write: paths("/srv/ws/demo/../other")}},
		{FS: &PathsPatch{Write: paths("relative")}},
	} {
		_, err := Narrow(base, patch)
		if err == nil {
			t.Fatalf("accepted unsafe filesystem patch: %#v", patch)
		}
	}
}

func TestNarrowEmptyListDeniesAll(t *testing.T) {
	base := basePolicy()
	got, err := Narrow(base, Patch{FS: &PathsPatch{
		Write: paths(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got.FS.Write.Unrestricted || len(got.FS.Write.Paths) != 0 {
		t.Fatalf("empty list did not deny writes: %#v", got.FS.Write)
	}
	if !reflect.DeepEqual(got.FS.Read.Paths, base.FS.Read.Paths) {
		t.Fatal("omitted read patch did not inherit")
	}
}

func TestNarrowLimitsAndNetwork(t *testing.T) {
	base := basePolicy()
	got, err := Narrow(base, Patch{Limits: LimitsPatch{WallSec: number(60)}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Limits.WallSec != 60 || got.Limits.MemoryMiB != 4096 {
		t.Fatalf("wrong effective limits: %#v", got.Limits)
	}
	for _, patch := range []Patch{
		{Limits: LimitsPatch{WallSec: number(301)}},
		{Limits: LimitsPatch{MemoryMiB: number(0)}},
		{Limits: LimitsPatch{PIDs: number(-1)}},
		{NetworkNone: boolPtr(false)},
		{KillOnOwnerExit: boolPtr(false)},
	} {
		if _, err := Narrow(base, patch); err == nil {
			t.Fatalf("accepted widening or invalid patch: %#v", patch)
		}
	}
	unrestricted := Policy{}
	narrowed, err := Narrow(unrestricted, Patch{NetworkNone: boolPtr(true)})
	if err != nil || !narrowed.NetworkNone {
		t.Fatalf("could not narrow unrestricted network: %#v, %v", narrowed, err)
	}
}

func TestNarrowEnvironment(t *testing.T) {
	base := basePolicy()
	got, err := Narrow(base, Patch{EnvAllow: paths("LANG")})
	if err != nil || !reflect.DeepEqual(got.Env.Allow, []string{"LANG"}) {
		t.Fatalf("wrong environment narrowing: %#v, %v", got.Env, err)
	}
	_, err = Narrow(base, Patch{EnvAllow: paths("LD_PRELOAD")})
	if !errors.Is(err, ErrWiden) {
		t.Fatalf("environment widening was not rejected: %v", err)
	}
}

func boolPtr(value bool) *bool { return &value }
