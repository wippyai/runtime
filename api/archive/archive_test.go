// SPDX-License-Identifier: MPL-2.0

package archive

import "testing"

type testCodec struct {
	name       string
	exts       []string
	sniffToken byte
}

func (c testCodec) Name() string { return c.name }

func (c testCodec) Extensions() []string { return c.exts }

func (c testCodec) Sniff(header []byte) bool {
	return len(header) > 0 && header[0] == c.sniffToken
}

func TestRegistryListGetAndResolve(t *testing.T) {
	Register(testCodec{name: "test-archive-short", exts: []string{".gz"}, sniffToken: 's'})
	Register(testCodec{name: "test-archive-long", exts: []string{".tar.gz"}, sniffToken: 'l'})
	Register(testCodec{name: "test-archive-explicit", exts: []string{".explicit"}, sniffToken: 'e'})

	if c, ok := Get("test-archive-explicit"); !ok || c.Name() != "test-archive-explicit" {
		t.Fatalf("Get explicit codec = %v ok=%v", c, ok)
	}

	names := List()
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("List is not sorted at %d: %q before %q", i, names[i-1], names[i])
		}
	}

	c, ok := Resolve("test-archive-explicit", "payload.tar.gz", []byte{'l'})
	if !ok || c.Name() != "test-archive-explicit" {
		t.Fatalf("explicit Resolve = %v ok=%v, want test-archive-explicit", c, ok)
	}

	c, ok = Resolve("", "payload.unknown", []byte{'l'})
	if !ok || c.Name() != "test-archive-long" {
		t.Fatalf("sniff Resolve = %v ok=%v, want test-archive-long", c, ok)
	}

	c, ok = Resolve("", "payload.tar.gz", nil)
	if !ok || c.Name() != "test-archive-long" {
		t.Fatalf("extension Resolve = %v ok=%v, want longest extension match", c, ok)
	}

	if c, ok := Resolve("test-archive-missing", "payload.tar.gz", []byte{'l'}); ok || c != nil {
		t.Fatalf("missing explicit Resolve = %v ok=%v, want nil false", c, ok)
	}
}
