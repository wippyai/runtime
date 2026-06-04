// SPDX-License-Identifier: MPL-2.0

package stream

import "testing"

// TestManifestMethodsAreRegistered proves the stream type manifest (StreamType,
// the typ.Interface exposed to the type system / docs) and the actually-registered
// Lua method table (streamMethods) describe the same method set.
//
// A method present in the manifest but absent from streamMethods is a "phantom":
// the type system advertises Stream:<method>, but the runtime never registers it,
// so calling it errors at runtime. That makes the manifest a stale, false
// description of the implementation.
func TestManifestMethodsAreRegistered(t *testing.T) {
	registered := make(map[string]bool, len(streamMethods))
	for name := range streamMethods {
		registered[name] = true
	}

	manifest := make(map[string]bool, len(StreamType.Methods))
	for _, m := range StreamType.Methods {
		manifest[m.Name] = true
		if !registered[m.Name] {
			t.Errorf("manifest declares Stream:%s but it is NOT registered in streamMethods (phantom method; calling it fails at runtime)", m.Name)
		}
	}

	for name := range streamMethods {
		if !manifest[name] {
			t.Errorf("streamMethods registers Stream:%s but the manifest omits it (undocumented method)", name)
		}
	}
}
