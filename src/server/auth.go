package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

const authCookieName = "offerpilot_auth"

type contextKey string

const ctxKeyUserID contextKey = "userId"

func userIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyUserID).(string); ok {
		return v
	}
	return ""
}

// requireAuth 校验登录态，通过后把 userID 写入请求上下文。
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		// 未初始化 MySQL / userService 时降级为放行（兼容无认证环境）
		if s.userService == nil {
			next(w, req)
			return
		}

		c, err := req.Cookie(authCookieName)
		if err != nil || c.Value == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		u, err := s.userService.ValidateSession(c.Value)
		if err != nil || u == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		ctx := context.WithValue(req.Context(), ctxKeyUserID, u.ID)
		next(w, req.WithContext(ctx))
	}
}

func (s *Server) handleRegister(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var body struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	body.Username = strings.TrimSpace(body.Username)
	body.Email = strings.TrimSpace(body.Email)
	if body.Username == "" || body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password required"})
		return
	}
	if len(body.Password) < 6 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password too short (min 6)"})
		return
	}

	u, err := s.userService.Register(body.Username, body.Email, body.Password)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	s.startAuthSession(w, u.ID)
	s.claimSession(req, u.ID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true,
		"user": map[string]string{"id": u.ID, "username": u.Username, "email": u.Email},
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	u, err := s.userService.Login(strings.TrimSpace(body.Username), body.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	s.startAuthSession(w, u.ID)
	s.claimSession(req, u.ID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true,
		"user": map[string]string{"id": u.ID, "username": u.Username, "email": u.Email},
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	if c, err := req.Cookie(authCookieName); err == nil && c.Value != "" {
		_ = s.userService.Logout(c.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:   authCookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, req *http.Request) {
	userID := userIDFromContext(req.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	u, err := s.userService.GetUserByID(userID)
	if err != nil || u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"id":       u.ID,
		"username": u.Username,
		"email":    u.Email,
	})
}

// startAuthSession 创建登录会话并下发 cookie。
func (s *Server) startAuthSession(w http.ResponseWriter, userID string) {
	sessionID, err := s.userService.CreateSession(userID)
	if err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 3600,
	})
}

// claimSession 登录后把当前匿名对话归属到用户。
func (s *Server) claimSession(req *http.Request, userID string) {
	if s.app == nil || s.app.SessionManager == nil {
		return
	}
	if c, err := req.Cookie("offerpilot_sid"); err == nil && c.Value != "" {
		_ = s.app.SessionManager.ClaimSession(c.Value, userID)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
