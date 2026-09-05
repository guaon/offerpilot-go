package server

import (
	"net/http"
	"os"
	"time"
)

func (s *Server) buildHandler() http.Handler {
	mux := http.NewServeMux()
	authLimiter := newRateLimiter(5, time.Minute)
	chatLimiter := newRateLimiter(20, time.Minute)
	expensiveLimiter := newRateLimiter(10, time.Minute)
	ipKey := func(req *http.Request) string { return clientIP(req, s.trustProxyHeaders) }
	withAuth := func(handler http.HandlerFunc) http.HandlerFunc { return s.optionalAuth(handler) }
	limited := func(handler http.HandlerFunc, maxBytes int64, limiter *rateLimiter) http.HandlerFunc {
		handler = bodyLimitMiddleware(maxBytes)(handler)
		if limiter != nil {
			handler = limiter.middleware(handler, s.rateLimitKey)
		}
		return withAuth(handler)
	}

	// Authentication endpoints share one IP-based budget to slow credential attacks.
	mux.HandleFunc("/api/register", authLimiter.middleware(bodyLimitMiddleware(64<<10)(s.handleRegister), ipKey))
	mux.HandleFunc("/api/login", authLimiter.middleware(bodyLimitMiddleware(64<<10)(s.handleLogin), ipKey))
	mux.HandleFunc("/api/logout", withAuth(s.handleLogout))
	mux.HandleFunc("/api/me", withAuth(s.handleMe))
	// Anonymous access is preserved; optionalAuth adds an identity when a valid cookie exists.
	mux.HandleFunc("/api/chat", limited(s.handleChat, 256<<10, chatLimiter))
	mux.HandleFunc("/api/session", withAuth(s.handleSession))
	mux.HandleFunc("/api/session/new", withAuth(s.handleSessionNew))
	mux.HandleFunc("/api/sessions", withAuth(s.handleSessions))
	mux.HandleFunc("/api/transcribe", limited(s.handleTranscribe, 20<<20, expensiveLimiter))
	mux.HandleFunc("/api/tts", limited(s.handleTTS, 1<<20, expensiveLimiter))
	mux.HandleFunc("/api/config", limited(s.handleConfig, 256<<10, nil))
	mux.HandleFunc("/api/diagnosis", limited(s.handleDiagnosis, 256<<10, nil))
	mux.HandleFunc("/api/match", limited(s.handleMatch, 2<<20, expensiveLimiter))
	mux.HandleFunc("/api/resume", limited(boundedConcurrencyMiddleware(4, s.handleResume), 12<<20, expensiveLimiter))
	mux.HandleFunc("/api/parse-pdf", limited(boundedConcurrencyMiddleware(4, s.handleParsePDF), 12<<20, expensiveLimiter))
	mux.HandleFunc("/api/parse-url", limited(boundedConcurrencyMiddleware(8, s.handleParseURL), 64<<10, expensiveLimiter))
	mux.HandleFunc("/api/_next/", s.handleStaticFile)
	mux.HandleFunc("/api/brand/", s.handleStaticFile)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/_next/", s.handleStaticFile)
	mux.HandleFunc("/brand/", s.handleStaticFile)
	mux.HandleFunc("/upload", s.handleUploadPage)
	mux.HandleFunc("/upload.js", s.handleUploadJS)
	mux.HandleFunc("/radar", s.handleRadar)
	mux.HandleFunc("/login", s.handleLoginPage)
	mux.HandleFunc("/", s.handleStaticOrSPA)

	handler := corsMiddleware(parseAllowedOrigins(os.Getenv("ALLOWED_ORIGIN")), mux)
	handler = securityHeadersMiddleware(handler)
	return recoveryMiddleware(handler)
}
