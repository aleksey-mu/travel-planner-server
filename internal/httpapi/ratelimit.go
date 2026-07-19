package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
)

// clientIP returns the client address for rate limiting.
// When trustProxy is true, prefers X-Real-IP or the first X-Forwarded-For hop
// (only safe behind a trusted reverse proxy that overwrites those headers).
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
			if host := normalizeIP(ip); host != "" {
				return host
			}
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if host := normalizeIP(parts[0]); host != "" {
				return host
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	if r.RemoteAddr != "" {
		return r.RemoteAddr
	}
	return "unknown"
}

func normalizeIP(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	return ""
}

// rateLimitIdentityHash returns a fixed-length key material for secrets (reset tokens).
func rateLimitIdentityHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// checkAuthRateLimit enforces separate IP and identity buckets. Returns false if limited.
func (a *API) checkAuthRateLimit(w http.ResponseWriter, r *http.Request, action, identity string) bool {
	ip := clientIP(r, a.cfg.TrustProxy)
	if !a.limiter.Allow(action + ":ip:" + ip) {
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests; try again later")
		return false
	}
	if id := strings.TrimSpace(identity); id != "" {
		if !a.limiter.Allow(action + ":id:" + id) {
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests; try again later")
			return false
		}
	}
	return true
}
