package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// rateLimiter 基于内存的简单 IP 速率限制器。
type rateLimiter struct {
	mu       sync.Mutex
	entries  map[string]*rateEntry
	rate     int           // 允许的请求数
	interval time.Duration // 时间窗口
}

type rateEntry struct {
	count   int
	resetAt time.Time
}

func newRateLimiter(rate int, interval time.Duration) *rateLimiter {
	rl := &rateLimiter{
		entries:  make(map[string]*rateEntry),
		rate:     rate,
		interval: interval,
	}
	// 定期清理过期条目
	go rl.cleanup()
	return rl
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, ok := rl.entries[ip]
	if !ok || now.After(entry.resetAt) {
		rl.entries[ip] = &rateEntry{count: 1, resetAt: now.Add(rl.interval)}
		return true
	}

	if entry.count >= rl.rate {
		return false
	}

	entry.count++
	return true
}

func (rl *rateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, entry := range rl.entries {
			if now.After(entry.resetAt) {
				delete(rl.entries, ip)
			}
		}
		rl.mu.Unlock()
	}
}

type requestKeyFunc func(*http.Request) string

func (rl *rateLimiter) middleware(next http.HandlerFunc, keyFn requestKeyFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		key := clientIP(req, false)
		if keyFn != nil {
			key = keyFn(req)
		}

		if !rl.allow(key) {
			w.Header().Set("Retry-After", "60")
			writeAPIError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "Too many requests", true)
			return
		}
		next(w, req)
	}
}

func clientIP(req *http.Request, trustProxy bool) string {
	address := req.RemoteAddr
	if trustProxy {
		if forwarded := req.Header.Get("X-Forwarded-For"); forwarded != "" {
			address = strings.TrimSpace(strings.Split(forwarded, ",")[0])
		} else if realIP := strings.TrimSpace(req.Header.Get("X-Real-IP")); realIP != "" {
			address = realIP
		}
	}
	if host, _, err := net.SplitHostPort(address); err == nil {
		return host
	}
	return address
}

// securityHeadersMiddleware 为所有响应添加安全头。
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(self), geolocation=()")
		next.ServeHTTP(w, req)
	})
}

// bodyLimitMiddleware 限制请求体大小（默认 1MB）。文件上传类接口不适用。
func bodyLimitMiddleware(maxBytes int64) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, req *http.Request) {
			if req.ContentLength > maxBytes {
				writeAPIError(w, http.StatusRequestEntityTooLarge, "request_too_large", "Request body is too large", false)
				return
			}
			req.Body = http.MaxBytesReader(w, req.Body, maxBytes)
			next(w, req)
		}
	}
}
