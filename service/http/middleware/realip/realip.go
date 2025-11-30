package realip

import (
	"net"
	"net/http"
	"strings"
)

const (
	MiddlewareName = "real_ip"

	// Option keys (dot-separated, preferred)
	optionTrustedSubnets = "real_ip.trusted.subnets"
	optionTrustAll       = "real_ip.trust_all"

	// Legacy option keys (deprecated, for backward compatibility)
	legacyTrustedSubnets = "trusted_subnets"

	// Header constants
	trueClientIP  = "True-Client-IP"
	xRealIP       = "X-Real-IP"
	xForwardedFor = "X-Forwarded-For"

	// Default trusted subnets:
	// - 127.0.0.0/8: IPv4 loopback
	// - 10.0.0.0/8: RFC 1918 private (Class A)
	// - 172.16.0.0/12: RFC 1918 private (Class B)
	// - 192.168.0.0/16: RFC 1918 private (Class C)
	// - 169.254.0.0/16: IPv4 link-local
	// - 100.64.0.0/10: CGNAT (RFC 6598, common in cloud environments)
	// - ::1/128: IPv6 loopback
	// - fc00::/7: IPv6 unique local addresses
	// - fe80::/10: IPv6 link-local
	defaultTrustedSubnets = "127.0.0.0/8,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,169.254.0.0/16,100.64.0.0/10,::1/128,fc00::/7,fe80::/10"
)

// getOption retrieves an option value, checking the new dot-separated key first,
// then falling back to the legacy underscore key for backward compatibility
func getOption(options map[string]string, newKey, legacyKey string) string {
	if val, ok := options[newKey]; ok {
		return val
	}
	return options[legacyKey]
}

// CreateRealIPMiddleware creates a middleware that sets a http.Request's RemoteAddr
// to the results of parsing either the True-Client-IP, X-Real-IP or the X-Forwarded-For
// headers (in that order).
//
// Options:
//   - real_ip.trusted.subnets: comma-separated list of CIDR blocks (e.g., "10.0.0.0/8,172.16.0.0/12")
//     Defaults to loopback addresses only (127.0.0.0/8,::1/128) for security.
//   - real_ip.trust_all: set to "true" to trust all sources (insecure, use with caution)
func CreateRealIPMiddleware(options map[string]string) func(http.Handler) http.Handler {
	var trustedSubnets []*net.IPNet

	// Check for explicit trust_all option
	if options[optionTrustAll] == "true" {
		trustedSubnets = nil // nil means trust all in shouldTrust
	} else {
		subnetsStr := getOption(options, optionTrustedSubnets, legacyTrustedSubnets)
		if subnetsStr == "" {
			subnetsStr = defaultTrustedSubnets
		}
		trustedSubnets = parseTrustedSubnets(subnetsStr)
	}

	trustAll := options[optionTrustAll] == "true"

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if trustAll || shouldTrust(r.RemoteAddr, trustedSubnets) {
				if rip := extractRealIP(r); rip != "" {
					r.RemoteAddr = rip
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// parseTrustedSubnets parses comma-separated CIDR blocks
func parseTrustedSubnets(subnets string) []*net.IPNet {
	if subnets == "" {
		return nil
	}

	cidrs := strings.Split(subnets, ",")
	var networks []*net.IPNet

	for _, cidr := range cidrs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}

		_, network, err := net.ParseCIDR(cidr)
		if err == nil {
			networks = append(networks, network)
		}
	}

	return networks
}

// shouldTrust checks if the remote address is in a trusted subnet
func shouldTrust(remoteAddr string, trustedSubnets []*net.IPNet) bool {
	if len(trustedSubnets) == 0 {
		return true
	}

	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	for _, network := range trustedSubnets {
		if network.Contains(ip) {
			return true
		}
	}

	return false
}

// extractRealIP extracts the real IP from headers
func extractRealIP(r *http.Request) string {
	var ip string

	if tcip := r.Header.Get(trueClientIP); tcip != "" {
		ip = tcip
	} else if xrip := r.Header.Get(xRealIP); xrip != "" {
		ip = xrip
	} else if xff := r.Header.Get(xForwardedFor); xff != "" {
		ip, _, _ = strings.Cut(xff, ",")
		ip = strings.TrimSpace(ip)
	}

	if ip == "" || net.ParseIP(ip) == nil {
		return ""
	}

	return ip
}
