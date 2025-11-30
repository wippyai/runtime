package filesystem

import (
	"testing"

	"github.com/wippyai/runtime/runtime/wasm/resource"
)

func TestConfig(t *testing.T) {
	t.Run("clone creates deep copy", func(t *testing.T) {
		orig := &Config{
			DefaultFS: "local",
			AllowedFS: []string{"local", "temp"},
			RootPath:  "/app",
		}

		cloned := orig.Clone().(*Config)

		if cloned.DefaultFS != orig.DefaultFS {
			t.Errorf("DefaultFS = %s, want %s", cloned.DefaultFS, orig.DefaultFS)
		}
		if cloned.RootPath != orig.RootPath {
			t.Errorf("RootPath = %s, want %s", cloned.RootPath, orig.RootPath)
		}

		// Verify deep copy of AllowedFS
		cloned.AllowedFS[0] = "modified"
		if orig.AllowedFS[0] == "modified" {
			t.Error("clone shares AllowedFS slice with original")
		}
	})

	t.Run("nil clone returns nil", func(t *testing.T) {
		var c *Config
		if c.Clone() != nil {
			t.Error("nil config clone should return nil")
		}
	})

	t.Run("IsAllowed with empty list allows all", func(t *testing.T) {
		c := &Config{DefaultFS: "local"}

		if !c.IsAllowed("local") {
			t.Error("expected local allowed")
		}
		if !c.IsAllowed("temp") {
			t.Error("expected temp allowed")
		}
	})

	t.Run("IsAllowed filters by list", func(t *testing.T) {
		c := &Config{
			DefaultFS: "local",
			AllowedFS: []string{"local", "app"},
		}

		if !c.IsAllowed("local") {
			t.Error("expected local allowed")
		}
		if !c.IsAllowed("app") {
			t.Error("expected app allowed")
		}
		if c.IsAllowed("temp") {
			t.Error("expected temp not allowed")
		}
	})

	t.Run("nil config allows all", func(t *testing.T) {
		var c *Config
		if !c.IsAllowed("anything") {
			t.Error("nil config should allow all")
		}
	})
}

func TestTypesHost(t *testing.T) {
	t.Run("creates with shared resources", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewTypesHost(res)

		if host.Resources() != res {
			t.Error("expected same resources instance")
		}
	})

	t.Run("info returns correct namespace", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewTypesHost(res)
		info := host.Info()

		if info.Namespace != TypesNamespace {
			t.Errorf("namespace = %s, want %s", info.Namespace, TypesNamespace)
		}
	})

	t.Run("register returns descriptor methods", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewTypesHost(res)
		reg := host.Register()

		expectedFuncs := []string{
			"[method]descriptor.get-type",
			"[method]descriptor.get-flags",
			"[method]descriptor.stat",
			"[method]descriptor.open-at",
			"[resource-drop]descriptor",
		}

		for _, name := range expectedFuncs {
			if _, ok := reg.Functions[name]; !ok {
				t.Errorf("missing function: %s", name)
			}
		}
	})

	t.Run("descriptors stored in shared table", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewTypesHost(res)

		desc := &Descriptor{
			FSID:  "local",
			Path:  "/app",
			IsDir: true,
			Flags: FlagRead,
		}
		handle := host.Descriptors().Insert(desc)

		if handle == 0 {
			t.Fatal("expected non-zero handle")
		}

		got, ok := host.Descriptors().Get(handle)
		if !ok {
			t.Fatal("expected descriptor")
		}
		if got.FSID != "local" {
			t.Errorf("FSID = %s, want local", got.FSID)
		}

		if res.Len() != 1 {
			t.Errorf("resource count = %d, want 1", res.Len())
		}
	})

	t.Run("descriptor dropper cleans up", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewTypesHost(res)

		desc := &Descriptor{
			FSID:  "local",
			Path:  "/app/file.txt",
			IsDir: false,
		}
		handle := host.Descriptors().Insert(desc)

		res.Table().Remove(handle)

		if desc.Path != "" {
			t.Error("expected path cleared after drop")
		}
		if desc.File != nil {
			t.Error("expected file closed after drop")
		}
	})
}

func TestPreopensHost(t *testing.T) {
	t.Run("creates with shared resources", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewPreopensHost(res)

		if host.Resources() != res {
			t.Error("expected same resources instance")
		}
	})

	t.Run("info returns correct namespace", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewPreopensHost(res)
		info := host.Info()

		if info.Namespace != PreopensNamespace {
			t.Errorf("namespace = %s, want %s", info.Namespace, PreopensNamespace)
		}
	})

	t.Run("register returns get-directories", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewPreopensHost(res)
		reg := host.Register()

		if _, ok := reg.Functions["get-directories"]; !ok {
			t.Error("missing get-directories function")
		}
	})
}

func TestDescriptorFlags(t *testing.T) {
	flags := FlagRead | FlagWrite

	if flags&FlagRead == 0 {
		t.Error("expected FlagRead set")
	}
	if flags&FlagWrite == 0 {
		t.Error("expected FlagWrite set")
	}
	if flags&FlagMutateDirectory != 0 {
		t.Error("expected FlagMutateDirectory not set")
	}
}

func TestDescriptorType(t *testing.T) {
	tests := []struct {
		t    DescriptorType
		want uint8
	}{
		{DescTypeUnknown, 0},
		{DescTypeDirectory, 3},
		{DescTypeRegularFile, 6},
	}

	for _, tt := range tests {
		if uint8(tt.t) != tt.want {
			t.Errorf("DescriptorType %d != %d", tt.t, tt.want)
		}
	}
}

func BenchmarkDescriptorInsertRemove(b *testing.B) {
	res := resource.NewInstanceResources()
	defer res.Close()

	host := NewTypesHost(res)
	desc := &Descriptor{FSID: "local", Path: "/"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := host.Descriptors().Insert(desc)
		res.Table().Remove(h)
	}
}
