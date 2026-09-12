// SPDX-License-Identifier: MPL-2.0

package eventual

import (
	"github.com/wippyai/runtime/api/relay"
	"testing"
)

type localRevokeRecorder struct{ packages []*relay.Package }

func (r *localRevokeRecorder) Send(p *relay.Package) error {
	r.packages = append(r.packages, p)
	return nil
}

func TestRevokeForStrongUsesLocalOrigin(t *testing.T) {
	for _, mode := range []string{"keep-local", "keep-local-cached", "keep-remote", "keep-other"} {
		t.Run(mode, func(t *testing.T) {
			recorder := &localRevokeRecorder{}
			s := NewService(Config{LocalNodeID: "local", Revoker: recorder})
			local := makePID("local", "h", "owner")
			remote := makePID("remote", "h", "winner")
			keep := makePID("third", "h", "strong")
			if mode == "keep-local" {
				keep = local
			}
			if mode == "keep-local-cached" {
				keep = local.Precomputed()
			}
			if mode == "keep-remote" {
				keep = remote
			}
			if _, err := s.Register("name", local); err != nil {
				t.Fatal(err)
			}
			// Apply directly to expose the hidden local dot without emitting the
			// independent remote-merge notification.
			s.state.Apply(&Entry{Name: "name", PID: remote, Node: s.state.internNode("remote"), Counter: 1, Priority: 100})
			if got, ok := s.state.Lookup("name"); !ok || got != remote {
				t.Fatal("fixture must have a remote winner")
			}
			revoked := s.RevokeForStrong("name", keep)
			want := !keep.Equal(local)
			if revoked != want {
				t.Fatalf("revoked=%v, want %v", revoked, want)
			}
			_, owned := s.owned["name"]
			if owned == want {
				t.Fatalf("owned=%v, revoked=%v", owned, want)
			}
			if want {
				if len(recorder.packages) != 1 || recorder.packages[0].Target != local {
					t.Fatal("must notify the local owner, not the remote winner")
				}
				if s.RevokeForStrong("name", keep) {
					t.Fatal("duplicate revocation")
				}
			} else if len(recorder.packages) != 0 {
				t.Fatal("same-owner binding must not be revoked")
			}
			// Removing the remote winner exposes whether the correct local dot survived.
			s.state.Apply(&Entry{Name: "name", Node: s.state.internNode("remote"), Counter: 2, Deleted: true})
			got, found := s.state.Lookup("name")
			if found != !want || (found && got != local) {
				t.Fatalf("surviving local binding=%v, found=%v", got, found)
			}
		})
	}
}

func TestWithdrawalDisarmsTombstonedOwnedIntent(t *testing.T) {
	for _, strong := range []bool{false, true} {
		s := NewService(Config{LocalNodeID: "local"})
		owner := makePID("local", "h", "owner")
		if _, err := s.Register("name", owner); err != nil {
			t.Fatal(err)
		}
		s.state.Unregister("name", 1)
		if strong {
			s.RevokeForStrong("name", makePID("remote", "h", "strong"))
		} else {
			s.Unregister("name")
		}
		s.reassertOwned("name")
		if _, found := s.state.Lookup("name"); found {
			t.Fatalf("withdrawn intent resurrected: strong=%v", strong)
		}
	}
}
