// SPDX-License-Identifier: MPL-2.0

// Package admin provides a read-only HTTP control-plane the chaos
// harness uses to inspect raft, gossip, and membership state across
// every runtime pod. The endpoints are stateless reads — no side
// effects, no auth surface beyond the gateway binding.
//
// Wired by boot/components/system/admin which discovers the required
// services via the app context. Dependencies are hard-required (the
// boot component fails if any are missing); handlers therefore don't
// guard against nil receivers.
package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/system/eventualreg"
	"github.com/wippyai/runtime/system/globalreg"
	sysraft "github.com/wippyai/runtime/system/raft"
	"go.uber.org/zap"
)

// Deps bundles the read-only services the admin server depends on.
// Every field is required — the boot component fails loud if any are
// missing rather than returning 503 per-request.
type Deps struct {
	EventualReg *eventualreg.Service
	GlobalReg   *globalreg.Service
	GlobalRaft  *sysraft.Node
	KVRaft      *sysraft.Node
	Membership  cluster.Membership
	Logger      *zap.Logger
}

// NewMux returns an http.ServeMux preconfigured with the admin routes.
func NewMux(deps Deps) *http.ServeMux {
	if deps.Logger == nil {
		deps.Logger = zap.NewNop()
	}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /admin/raft/status", raftHandler(deps, "status",
		func(n *sysraft.Node) any {
			leaderID, leaderAddr, _ := n.Leader()
			return raftStatusResp{
				State:       n.State().String(),
				LeaderID:    leaderID,
				LeaderAddr:  leaderAddr,
				LastContact: n.LastContact().UTC().Format(time.RFC3339Nano),
				Now:         time.Now().UTC().Format(time.RFC3339Nano),
				IsLeader:    n.IsLeader(),
				IsVoter:     n.IsVoter(),
				Term:        n.Term(),
				CommitIndex: n.CommitIndex(),
			}
		}))

	mux.HandleFunc("GET /admin/raft/log-head", raftHandler(deps, "log-head", nil))

	mux.HandleFunc("GET /admin/raft/configuration", raftHandler(deps, "configuration",
		func(n *sysraft.Node) any {
			servers, err := n.GetConfiguration()
			if err != nil {
				return errorResp{Error: err.Error()}
			}
			out := raftConfigResp{}
			for _, s := range servers {
				out.Servers = append(out.Servers, raftConfigServer{
					ID: s.ID, Address: s.Address, IsVoter: s.IsVoter,
				})
			}
			return out
		}))

	mux.HandleFunc("GET /admin/eventualreg/digest", func(w http.ResponseWriter, _ *http.Request) {
		dig := deps.EventualReg.LocalDigest()
		shards := make([]uint64, len(dig.Hashes))
		copy(shards, dig.Hashes[:])
		writeJSON(w, http.StatusOK, eventualregDigestResp{
			CV:          deps.EventualReg.CVSnapshot(),
			ShardHashes: shards,
		})
	})

	mux.HandleFunc("GET /admin/globalreg/pending", func(w http.ResponseWriter, _ *http.Request) {
		if deps.GlobalReg == nil {
			writeError(w, http.StatusServiceUnavailable, "globalreg not available")
			return
		}
		pending := deps.GlobalReg.PendingSnapshot()
		expired := deps.GlobalReg.ExpiredHistory()
		writeJSON(w, http.StatusOK, globalregPendingResp{
			Pending: pending,
			Expired: expired,
		})
	})

	mux.HandleFunc("GET /admin/membership/members", func(w http.ResponseWriter, _ *http.Request) {
		nodes := deps.Membership.Nodes()
		out := membershipResp{
			Local:   deps.Membership.LocalNode().ID,
			Members: make([]membershipMemberResp, 0, len(nodes)),
		}
		for _, n := range nodes {
			out.Members = append(out.Members, membershipMemberResp{ID: n.ID, Addr: n.Addr})
		}
		writeJSON(w, http.StatusOK, out)
	})

	return mux
}

// raftHandler is the shared dispatch wrapper for the three raft
// endpoints. It resolves `?group=global|kv` to the right Node, then
// invokes the per-endpoint fn — except for log-head which has a
// query-parameter path of its own.
func raftHandler(d Deps, op string, fn func(*sysraft.Node) any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		group := strings.ToLower(r.URL.Query().Get("group"))
		if group == "" {
			group = "global"
		}
		var node *sysraft.Node
		switch group {
		case "global":
			node = d.GlobalRaft
		case "kv", "kvraft":
			node = d.KVRaft
		default:
			writeError(w, http.StatusBadRequest, "unknown raft group: "+group)
			return
		}
		if node == nil {
			writeError(w, http.StatusServiceUnavailable, "raft group not available: "+group)
			return
		}
		// log-head reads a count query param and is special-cased here
		// rather than via fn(node) so the dispatcher stays generic.
		if op == "log-head" {
			n := 32
			if v := r.URL.Query().Get("n"); v != "" {
				parsed, err := strconv.Atoi(v)
				if err != nil || parsed <= 0 {
					writeError(w, http.StatusBadRequest, "invalid n")
					return
				}
				if parsed > 1024 {
					parsed = 1024
				}
				n = parsed
			}
			entries, err := node.LogHead(n)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "log-head: "+err.Error())
				return
			}
			writeJSON(w, http.StatusOK, raftLogHeadResp{Group: group, Entries: entries})
			return
		}
		out := fn(node)
		if r, ok := out.(raftStatusResp); ok {
			r.Group = group
			out = r
		}
		if c, ok := out.(raftConfigResp); ok {
			c.Group = group
			out = c
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// --- response envelopes ---

type errorResp struct {
	Error string `json:"error"`
}

type raftStatusResp struct {
	Group       string `json:"group"`
	State       string `json:"state"`
	LeaderID    string `json:"leader_id"`
	LeaderAddr  string `json:"leader_addr"`
	LastContact string `json:"last_contact"`
	Now         string `json:"now"`
	IsLeader    bool   `json:"is_leader"`
	IsVoter     bool   `json:"is_voter"`
	Term        uint64 `json:"term"`
	CommitIndex uint64 `json:"commit_index"`
}

type raftLogHeadResp struct {
	Group   string                 `json:"group"`
	Entries []sysraft.LogHeadEntry `json:"entries"`
}

type raftConfigServer struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	IsVoter bool   `json:"is_voter"`
}

type raftConfigResp struct {
	Group   string             `json:"group"`
	Servers []raftConfigServer `json:"servers"`
}

type eventualregDigestResp struct {
	CV          []uint64 `json:"cv"`
	ShardHashes []uint64 `json:"shard_hashes"`
}

type globalregPendingResp struct {
	Pending []globalreg.PendingView   `json:"pending"`
	Expired []globalreg.ExpiredRecord `json:"expired"`
}

type membershipMemberResp struct {
	ID   string `json:"id"`
	Addr string `json:"addr"`
}

type membershipResp struct {
	Local   string                 `json:"local"`
	Members []membershipMemberResp `json:"members"`
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResp{Error: msg})
}
