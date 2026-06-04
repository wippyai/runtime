// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"errors"
	"testing"
)

// TestConfigValidate_NamespaceReservation pins the prefix-separation guarantee:
// app namespaces match ^[a-z][a-z0-9._-]*$ so they can never collide with the
// reserved _sys:* system namespace, and the key separator ':' is excluded so a
// namespace can never span into another store's keyspace. Both store.kv kinds
// share the rule; durability is orthogonal to it.
func TestConfigValidate_NamespaceReservation(t *testing.T) {
	reserved := []string{"_sys", "_sys:raft", "_sys:registry", "_lock", "_"}
	malformed := []string{"", "1bad", "BadCase", "a:b", "ns/with/slash", "has space", "UPPER"}
	valid := []string{"deploy", "sess", "kb", "kb_index", "good.ns", "ok-ns", "a"}

	check := func(t *testing.T, name string, validate func(ns string) error) {
		t.Helper()
		for _, ns := range append(append([]string{}, reserved...), malformed...) {
			if err := validate(ns); !errors.Is(err, ErrInvalidNamespace) {
				t.Errorf("%s namespace %q: got err=%v, want ErrInvalidNamespace", name, ns, err)
			}
		}
		for _, ns := range valid {
			if err := validate(ns); err != nil {
				t.Errorf("%s namespace %q: got err=%v, want nil", name, ns, err)
			}
		}
	}

	t.Run("raft", func(t *testing.T) {
		check(t, "raft", func(ns string) error {
			c := RaftConfig{Namespace: ns}
			return c.Validate()
		})
	})

	t.Run("crdt is identical and orthogonal to durability", func(t *testing.T) {
		for _, durable := range []bool{false, true} {
			check(t, "crdt", func(ns string) error {
				c := CRDTConfig{Namespace: ns, Durable: durable}
				return c.Validate()
			})
		}
	})
}
