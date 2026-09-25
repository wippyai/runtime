// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
)

// MetadataCandidatesV3 is an additive, bounded membership hint. Older peers
// continue to use internode_port and the optional v2 advertised endpoint.
const MetadataCandidatesV3 = "internode_candidates_v3"
const MetadataGossipCandidatesV3 = "gossip_candidates_v3"

const MaxCandidates = 8
const maxCandidateMetadata = 320 // leave room in memberlist's 512-byte node meta

// ParseCandidateList accepts comma-separated host or host:port endpoints.
// The listener port is used when an entry does not specify a port.
func ParseCandidateList(raw string, defaultPort int) ([]Candidate, error) {
	var out []Candidate
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		host, port := item, defaultPort
		if h, p, err := net.SplitHostPort(item); err == nil {
			host = h
			port, err = strconv.Atoi(p)
			if err != nil {
				return nil, fmt.Errorf("invalid candidate port %q", item)
			}
		} else if strings.Contains(item, ":") && net.ParseIP(strings.Trim(item, "[]")) == nil {
			return nil, fmt.Errorf("invalid candidate endpoint %q", item)
		}
		candidate, ok := classifyCandidate(Candidate{Host: host, Port: port}, true)
		if !ok {
			return nil, fmt.Errorf("invalid remote candidate %q", item)
		}
		out = append(out, candidate)
		if len(out) > MaxCandidates {
			return nil, fmt.Errorf("too many internode candidates")
		}
	}
	return NormalizeCandidates(out), nil
}

// EncodeCandidatesForMeta fits the largest prefix possible alongside existing
// metadata. Memberlist rejects the entire node meta once it exceeds 512 bytes.
func EncodeCandidatesForMeta(existing map[string]string, candidates []Candidate) (string, error) {
	return EncodeCandidatesForMetaKey(existing, MetadataCandidatesV3, candidates)
}

func EncodeCandidatesForMetaKey(existing map[string]string, key string, candidates []Candidate) (string, error) {
	clean := NormalizeCandidates(candidates)
	for n := len(clean); n > 0; n-- {
		encoded, err := EncodeCandidates(clean[:n])
		if err != nil {
			continue
		}
		copyMeta := make(map[string]string, len(existing)+1)
		for k, v := range existing {
			copyMeta[k] = v
		}
		copyMeta[key] = encoded
		data, err := json.Marshal(copyMeta)
		if err != nil {
			return "", err
		}
		if len(data) <= 512 {
			return encoded, nil
		}
	}
	return "", nil
}

// Candidate describes a listener, not an identity or an authorization grant.
// Family is ipv4, ipv6, or dns; Scope is public, private, bridge, tailnet, or dns.
type Candidate struct {
	Host   string `json:"h"`
	Port   int    `json:"p"`
	Family string `json:"f"`
	Scope  string `json:"s"`
}

func (c Candidate) Address() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }

func classifyCandidate(c Candidate, remote bool) (Candidate, bool) {
	c.Host = strings.TrimSpace(strings.Trim(c.Host, "[]"))
	if c.Port < 1 || c.Port > 65535 || !ValidEndpointHost(c.Host) {
		return Candidate{}, false
	}
	if ip, err := netip.ParseAddr(c.Host); err == nil {
		originalScope := c.Scope
		ip = ip.Unmap()
		if !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || (remote && ip.IsLoopback()) {
			return Candidate{}, false
		}
		c.Host = ip.String()
		if ip.Is4() {
			c.Family = "ipv4"
		} else {
			c.Family = "ipv6"
		}
		if netip.MustParsePrefix("100.64.0.0/10").Contains(ip) || netip.MustParsePrefix("fd7a:115c:a1e0::/48").Contains(ip) {
			c.Scope = "tailnet"
		} else if ip.IsPrivate() || ip.IsLoopback() {
			c.Scope = "private"
			if originalScope == "bridge" {
				c.Scope = "bridge"
			}
		} else {
			c.Scope = "public"
		}
	} else {
		c.Host = strings.ToLower(strings.TrimSuffix(c.Host, "."))
		c.Family, c.Scope = "dns", "dns"
		if strings.HasSuffix(c.Host, ".ts.net") || strings.HasSuffix(c.Host, ".tailscale.net") {
			c.Scope = "tailnet"
		}
	}
	return c, true
}

func normalizeCandidates(in []Candidate, remote bool) []Candidate {
	out := make([]Candidate, 0, min(len(in), MaxCandidates))
	seen := make(map[string]bool)
	for _, candidate := range in {
		c, ok := classifyCandidate(candidate, remote)
		if !ok || seen[c.Address()] {
			continue
		}
		seen[c.Address()] = true
		out = append(out, c)
		if len(out) == MaxCandidates {
			break
		}
	}
	return out
}

// NormalizeCandidates drops addresses that cannot be advertised to remote peers.
func NormalizeCandidates(in []Candidate) []Candidate { return normalizeCandidates(in, true) }

// DiscoverCandidates places explicit addresses first, then active interface
// addresses. It never advertises loopback or link-local listeners remotely.
func DiscoverCandidates(configured []Candidate, bindAddr string, port int) []Candidate {
	in := append([]Candidate(nil), configured...)
	bindIP := net.ParseIP(bindAddr)
	if bindIP == nil || !bindIP.IsUnspecified() {
		return NormalizeCandidates(in)
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return NormalizeCandidates(in)
	}
	var discovered []Candidate
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if p, err := netip.ParsePrefix(addr.String()); err == nil {
				if bindIP.To4() != nil && !p.Addr().Is4() {
					continue
				}
				candidate := Candidate{Host: p.Addr().String(), Port: port}
				name := strings.ToLower(iface.Name)
				if strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "br-") || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "cni") {
					candidate.Scope = "bridge"
				}
				if typed, ok := classifyCandidate(candidate, true); ok {
					discovered = append(discovered, typed)
				}
			}
		}
	}
	sort.SliceStable(discovered, func(i, j int) bool {
		return candidateScopeRank(discovered[i].Scope) < candidateScopeRank(discovered[j].Scope)
	})
	return NormalizeCandidates(append(in, discovered...))
}

func candidateScopeRank(scope string) int {
	switch scope {
	case "tailnet":
		return 0
	case "public":
		return 1
	case "private", "dns":
		return 2
	default:
		return 3
	}
}

// OrderCandidates prefers the last authenticated path, then alternates IPv6
// and IPv4 so a slow family cannot hold up every address of the other family.
func OrderCandidates(in []Candidate, pinned Candidate) []Candidate {
	pool := normalizeCandidates(in, false)
	out := make([]Candidate, 0, len(pool))
	for i, c := range pool {
		if c.Address() == pinned.Address() && pinned.Host != "" {
			out = append(out, c)
			pool = append(pool[:i], pool[i+1:]...)
			break
		}
	}
	var v4, v6, dns []Candidate
	for _, c := range pool {
		switch c.Family {
		case "ipv4":
			v4 = append(v4, c)
		case "ipv6":
			v6 = append(v6, c)
		default:
			dns = append(dns, c)
		}
	}
	first4 := len(pool) > 0 && pool[0].Family == "ipv4"
	for len(v4)+len(v6) > 0 {
		if first4 {
			if len(v4) > 0 {
				out = append(out, v4[0])
				v4 = v4[1:]
			}
			if len(v6) > 0 {
				out = append(out, v6[0])
				v6 = v6[1:]
			}
		} else {
			if len(v6) > 0 {
				out = append(out, v6[0])
				v6 = v6[1:]
			}
			if len(v4) > 0 {
				out = append(out, v4[0])
				v4 = v4[1:]
			}
		}
	}
	return append(out, dns...)
}

type candidateWire struct {
	Version    int        `json:"v"`
	Candidates [][]string `json:"c"`
}

func familyCode(f string) string {
	switch f {
	case "ipv4":
		return "4"
	case "ipv6":
		return "6"
	default:
		return "d"
	}
}
func scopeCode(s string) string {
	switch s {
	case "tailnet":
		return "t"
	case "private":
		return "r"
	case "bridge":
		return "b"
	case "public":
		return "p"
	default:
		return "d"
	}
}

func EncodeCandidates(in []Candidate) (string, error) {
	clean := NormalizeCandidates(in)
	wire := candidateWire{Version: 3}
	for _, c := range clean {
		wire.Candidates = append(wire.Candidates, []string{c.Host, strconv.Itoa(c.Port), familyCode(c.Family), scopeCode(c.Scope)})
	}
	data, err := json.Marshal(wire)
	if err != nil {
		return "", err
	}
	if len(data) > maxCandidateMetadata {
		return "", fmt.Errorf("internode candidates exceed %d bytes", maxCandidateMetadata)
	}
	return string(data), nil
}

func DecodeCandidates(encoded string) ([]Candidate, error) {
	if len(encoded) > maxCandidateMetadata {
		return nil, fmt.Errorf("internode candidates exceed %d bytes", maxCandidateMetadata)
	}
	var wire candidateWire
	if err := json.Unmarshal([]byte(encoded), &wire); err != nil {
		return nil, err
	}
	if wire.Version != 3 || len(wire.Candidates) > MaxCandidates {
		return nil, fmt.Errorf("unsupported internode candidates version or count")
	}
	var candidates []Candidate
	for _, fields := range wire.Candidates {
		if len(fields) != 4 {
			return nil, fmt.Errorf("invalid internode candidate fields")
		}
		port, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("invalid internode candidate port")
		}
		c, ok := classifyCandidate(Candidate{Host: fields[0], Port: port, Scope: map[string]string{"b": "bridge"}[fields[3]]}, true)
		if !ok || fields[2] != familyCode(c.Family) || fields[3] != scopeCode(c.Scope) {
			return nil, fmt.Errorf("invalid internode candidate")
		}
		candidates = append(candidates, c)
	}
	return NormalizeCandidates(candidates), nil
}
