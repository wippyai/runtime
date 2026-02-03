package realip

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateRealIPMiddleware(t *testing.T) {
	t.Run("extract True-Client-IP from loopback", func(t *testing.T) {
		middleware := CreateRealIPMiddleware(map[string]string{})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "1.2.3.4", r.RemoteAddr)
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "127.0.0.1:1234" // loopback is trusted by default
		req.Header.Set("True-Client-IP", "1.2.3.4")
		req.Header.Set("X-Real-IP", "5.6.7.8")
		req.Header.Set("X-Forwarded-For", "9.10.11.12")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("extract X-Real-IP when True-Client-IP not present", func(t *testing.T) {
		middleware := CreateRealIPMiddleware(map[string]string{})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "5.6.7.8", r.RemoteAddr)
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "127.0.0.1:1234" // loopback is trusted by default
		req.Header.Set("X-Real-IP", "5.6.7.8")
		req.Header.Set("X-Forwarded-For", "9.10.11.12")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("extract X-Forwarded-For when others not present", func(t *testing.T) {
		middleware := CreateRealIPMiddleware(map[string]string{})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "9.10.11.12", r.RemoteAddr)
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "127.0.0.1:1234" // loopback is trusted by default
		req.Header.Set("X-Forwarded-For", "9.10.11.12")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("extract first IP from X-Forwarded-For list", func(t *testing.T) {
		middleware := CreateRealIPMiddleware(map[string]string{})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "9.10.11.12", r.RemoteAddr)
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "127.0.0.1:1234" // loopback is trusted by default
		req.Header.Set("X-Forwarded-For", "9.10.11.12, 13.14.15.16, 17.18.19.20")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("no real IP headers - keep original", func(t *testing.T) {
		middleware := CreateRealIPMiddleware(map[string]string{})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "127.0.0.1:1234", r.RemoteAddr)
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "127.0.0.1:1234"

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("invalid IP in header - keep original", func(t *testing.T) {
		middleware := CreateRealIPMiddleware(map[string]string{})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "127.0.0.1:1234", r.RemoteAddr)
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Real-IP", "not-an-ip")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("private IP source trusted by default", func(t *testing.T) {
		middleware := CreateRealIPMiddleware(map[string]string{})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "1.2.3.4", r.RemoteAddr) // header IP used
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "192.168.1.1:1234" // RFC 1918, trusted by default
		req.Header.Set("X-Real-IP", "1.2.3.4")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("public IP source not trusted by default", func(t *testing.T) {
		middleware := CreateRealIPMiddleware(map[string]string{})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "203.0.113.1:1234", r.RemoteAddr) // unchanged, public IP not trusted
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.1:1234" // public IP, not trusted
		req.Header.Set("X-Real-IP", "1.2.3.4")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("trust_all option trusts any source", func(t *testing.T) {
		middleware := CreateRealIPMiddleware(map[string]string{
			"real_ip.trust_all": "true",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "1.2.3.4", r.RemoteAddr)
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "192.168.1.1:1234" // would normally be untrusted
		req.Header.Set("X-Real-IP", "1.2.3.4")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestTrustedSubnets(t *testing.T) {
	t.Run("trust request from trusted subnet", func(t *testing.T) {
		middleware := CreateRealIPMiddleware(map[string]string{
			"trusted_subnets": "10.0.0.0/8,172.16.0.0/12",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "1.2.3.4", r.RemoteAddr)
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.1.2.3:1234"
		req.Header.Set("X-Real-IP", "1.2.3.4")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("don't trust request from untrusted subnet", func(t *testing.T) {
		middleware := CreateRealIPMiddleware(map[string]string{
			"trusted_subnets": "10.0.0.0/8",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "192.168.1.1:1234", r.RemoteAddr)
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		req.Header.Set("X-Real-IP", "1.2.3.4")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("empty trusted_subnets defaults to loopback only", func(t *testing.T) {
		middleware := CreateRealIPMiddleware(map[string]string{})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Non-loopback source should NOT be trusted
			assert.Equal(t, "203.0.113.1:1234", r.RemoteAddr)
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.1:1234"
		req.Header.Set("X-Real-IP", "1.2.3.4")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("IPv6 trusted subnet", func(t *testing.T) {
		middleware := CreateRealIPMiddleware(map[string]string{
			"trusted_subnets": "::1/128,fc00::/7",
		})

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "2001:db8::1", r.RemoteAddr)
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "[fc00::1]:1234"
		req.Header.Set("X-Real-IP", "2001:db8::1")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestParseTrustedSubnets(t *testing.T) {
	t.Run("parse valid CIDR blocks", func(t *testing.T) {
		subnets := parseTrustedSubnets("10.0.0.0/8,172.16.0.0/12,192.168.0.0/16")
		require.Len(t, subnets, 3)
	})

	t.Run("parse with spaces", func(t *testing.T) {
		subnets := parseTrustedSubnets("10.0.0.0/8 , 172.16.0.0/12 , 192.168.0.0/16")
		require.Len(t, subnets, 3)
	})

	t.Run("skip invalid CIDR blocks", func(t *testing.T) {
		subnets := parseTrustedSubnets("10.0.0.0/8,invalid,172.16.0.0/12")
		require.Len(t, subnets, 2)
	})

	t.Run("empty string returns nil", func(t *testing.T) {
		subnets := parseTrustedSubnets("")
		assert.Nil(t, subnets)
	})
}

func TestShouldTrust(t *testing.T) {
	_, subnet1, _ := net.ParseCIDR("10.0.0.0/8")
	_, subnet2, _ := net.ParseCIDR("172.16.0.0/12")
	subnets := []*net.IPNet{subnet1, subnet2}

	t.Run("trust IP in subnet", func(t *testing.T) {
		assert.True(t, shouldTrust("10.1.2.3:1234", subnets))
	})

	t.Run("don't trust IP outside subnet", func(t *testing.T) {
		assert.False(t, shouldTrust("192.168.1.1:1234", subnets))
	})

	t.Run("trust all when no subnets", func(t *testing.T) {
		assert.True(t, shouldTrust("192.168.1.1:1234", nil))
	})

	t.Run("handle IP without port", func(t *testing.T) {
		assert.True(t, shouldTrust("10.1.2.3", subnets))
	})

	t.Run("reject invalid IP", func(t *testing.T) {
		assert.False(t, shouldTrust("not-an-ip", subnets))
	})
}

func TestExtractRealIP(t *testing.T) {
	t.Run("extract from True-Client-IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("True-Client-IP", "1.2.3.4")
		assert.Equal(t, "1.2.3.4", extractRealIP(req))
	})

	t.Run("extract from X-Real-IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Real-IP", "5.6.7.8")
		assert.Equal(t, "5.6.7.8", extractRealIP(req))
	})

	t.Run("extract from X-Forwarded-For", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "9.10.11.12")
		assert.Equal(t, "9.10.11.12", extractRealIP(req))
	})

	t.Run("extract first from X-Forwarded-For list", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "9.10.11.12, 13.14.15.16")
		assert.Equal(t, "9.10.11.12", extractRealIP(req))
	})

	t.Run("return empty for no headers", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		assert.Equal(t, "", extractRealIP(req))
	})

	t.Run("return empty for invalid IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Real-IP", "not-an-ip")
		assert.Equal(t, "", extractRealIP(req))
	})
}
