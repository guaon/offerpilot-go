package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func writeAPIError(w http.ResponseWriter, status int, code, message string, retryable bool) {
	writeJSON(w, status, map[string]interface{}{
		"error": map[string]interface{}{
			"code":      code,
			"message":   message,
			"retryable": retryable,
		},
	})
}

func decodeJSON(w http.ResponseWriter, req *http.Request, dst interface{}) bool {
	decoder := json.NewDecoder(req.Body)
	if err := decoder.Decode(dst); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "request_too_large", "Request body is too large", false)
			return false
		}
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON request body", false)
		return false
	}
	return true
}

func isRequestTooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("recovered HTTP panic: %v", recovered)
				writeAPIError(w, http.StatusInternalServerError, "internal_error", "Internal server error", true)
			}
		}()
		next.ServeHTTP(w, req)
	})
}

func parseAllowedOrigins(value string) map[string]struct{} {
	origins := make(map[string]struct{})
	for _, origin := range strings.Split(value, ",") {
		origin = strings.TrimRight(strings.TrimSpace(origin), "/")
		if origin != "" {
			origins[origin] = struct{}{}
		}
	}
	return origins
}

func requestOriginAllowed(req *http.Request, origin string, allowed map[string]struct{}) bool {
	if _, ok := allowed[strings.TrimRight(origin, "/")]; ok {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	requestScheme := req.URL.Scheme
	if requestScheme == "" {
		requestScheme = "http"
		if req.TLS != nil {
			requestScheme = "https"
		}
	}
	return strings.EqualFold(parsed.Scheme, requestScheme) && strings.EqualFold(parsed.Host, req.Host)
}

func corsMiddleware(allowed map[string]struct{}, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		origin := req.Header.Get("Origin")
		originAllowed := origin != "" && requestOriginAllowed(req, origin, allowed)

		w.Header().Add("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-File-Name")
		if originAllowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		if req.Method == http.MethodOptions {
			if origin == "" || !originAllowed {
				writeAPIError(w, http.StatusForbidden, "origin_not_allowed", "Origin is not allowed", false)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if isUnsafeMethod(req.Method) && origin != "" && !originAllowed {
			if cookie, err := req.Cookie(authCookieName); err == nil && cookie.Value != "" {
				writeAPIError(w, http.StatusForbidden, "origin_not_allowed", "Origin is not allowed", false)
				return
			}
		}
		next.ServeHTTP(w, req)
	})
}

func isUnsafeMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions && method != http.MethodTrace
}

func boundedConcurrencyMiddleware(maxConcurrent int, next http.HandlerFunc) http.HandlerFunc {
	if maxConcurrent < 1 {
		return next
	}
	semaphore := make(chan struct{}, maxConcurrent)
	return func(w http.ResponseWriter, req *http.Request) {
		select {
		case semaphore <- struct{}{}:
			defer func() { <-semaphore }()
			next(w, req)
		default:
			writeAPIError(w, http.StatusServiceUnavailable, "server_busy", "Server is busy", true)
		}
	}
}

func (s *Server) rateLimitKey(req *http.Request) string {
	if userID := userIDFromContext(req.Context()); userID != "" {
		return "user:" + userID
	}
	return "ip:" + clientIP(req, s.trustProxyHeaders)
}

func envBool(name string) bool {
	value := strings.TrimSpace(os.Getenv(name))
	return strings.EqualFold(value, "true") || value == "1"
}
