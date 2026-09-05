package server

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"MyOfferPilot/src/app"
	"MyOfferPilot/src/db"
	"MyOfferPilot/src/logger"
	"MyOfferPilot/src/memory"
	"MyOfferPilot/src/session"
	"MyOfferPilot/src/user"

	"github.com/cloudwego/eino/schema"
	pdfreader "github.com/ledongthuc/pdf"
)

//go:embed web/*
var webFiles embed.FS

const (
	HeartbeatInterval = 15 * time.Second
)

type Server struct {
	port              string
	apiKey            string
	httpServer        *http.Server
	app               *app.App
	userService       *user.Service
	mysqlDB           *sql.DB
	diagnoses         *diagnosisStore
	diagnosisSvc      *DiagnosisService
	trustProxyHeaders bool
	mu                sync.Mutex
}

type ChatRequest struct {
	Message   string `json:"message"`
	SessionID string `json:"sessionId"`
	Model     string `json:"model"`
}

type ChatEvent struct {
	Type      string                 `json:"type"`
	Content   string                 `json:"content,omitempty"`
	Action    string                 `json:"action,omitempty"`
	Reason    string                 `json:"reason,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	Result    string                 `json:"result,omitempty"`
	SessionID string                 `json:"sessionId,omitempty"`
	Usage     map[string]int         `json:"usage,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

func NewServer(port string) *Server {
	if port == "" {
		port = "3001"
	}
	s := &Server{
		port:              port,
		apiKey:            os.Getenv("OFFERPILOT_API_KEY"),
		diagnoses:         newDiagnosisStore(),
		trustProxyHeaders: envBool("TRUST_PROXY_HEADERS"),
	}
	s.initAuth()
	s.diagnosisSvc = s.newDiagnosisService()
	return s
}

// initAuth 初始化 MySQL 连接与用户服务。MySQL 不可用时降级为无认证模式。
func (s *Server) initAuth() {
	conn, err := db.Open()
	if err != nil {
		logger.DefaultLogger.Warn("MySQL not available, auth disabled", map[string]interface{}{"error": err.Error()})
		return
	}
	if err := db.Migrate(conn); err != nil {
		logger.DefaultLogger.Warn("MySQL migrate failed", map[string]interface{}{"error": err.Error()})
		return
	}
	s.userService = user.NewService(user.NewStore(conn))
	s.mysqlDB = conn
	logger.DefaultLogger.Info("Auth initialized (MySQL)")
}

func (s *Server) Start() error {
	s.loadDiagnoses()

	s.mu.Lock()
	if s.app == nil {
		s.app = app.CreateApp(&app.AppOptions{DB: s.mysqlDB})
	}
	s.mu.Unlock()

	s.httpServer = s.newHTTPServer(s.buildHandler())

	logger.DefaultLogger.Info("Server starting", map[string]interface{}{
		"port": s.port,
		"auth": s.apiKey != "",
	})

	return s.httpServer.ListenAndServe()
}

func (s *Server) newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              ":" + s.port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

func (s *Server) Stop() error {
	logger.DefaultLogger.Info("Server stopping")
	if s.httpServer == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) validateAuth(req *http.Request) bool {
	if s.apiKey == "" {
		return true
	}
	authHeader := req.Header.Get("Authorization")
	if authHeader == "" {
		return false
	}
	return authHeader == "Bearer "+s.apiKey
}

func (s *Server) cors(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-File-Name")
}

func (s *Server) canAccessSession(req *http.Request, sessionID, userID string) bool {
	if sessionID == "" || s.app == nil || s.app.SessionManager == nil {
		return false
	}
	if !s.app.SessionManager.CanAccess(sessionID, userID) {
		return false
	}
	if userID != "" {
		return true
	}
	cookie, err := req.Cookie("offerpilot_sid")
	return err == nil && cookie.Value == sessionID
}

func (s *Server) diagnosisState() *diagnosisStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.diagnoses == nil {
		s.diagnoses = newDiagnosisStore()
	}
	return s.diagnoses
}

func (s *Server) diagnosisService() *DiagnosisService {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.diagnosisSvc == nil {
		if s.diagnoses == nil {
			s.diagnoses = newDiagnosisStore()
		}
		s.diagnosisSvc = s.newDiagnosisService()
	}
	return s.diagnosisSvc
}

// newDiagnosisService must only be called while constructing the server or
// while s.mu is held.
func (s *Server) newDiagnosisService() *DiagnosisService {
	return NewDiagnosisService(
		s.diagnoses,
		newMySQLDiagnosisRepository(s.mysqlDB),
		DiagnosisProjectorFunc(s.projectDiagnosis),
	)
}

func (s *Server) handleChat(w http.ResponseWriter, req *http.Request) {
	s.cors(w)

	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.validateAuth(req) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"})
		return
	}

	var chatReq ChatRequest
	if !decodeJSON(w, req, &chatReq) {
		return
	}

	if chatReq.Message == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "message is required"})
		return
	}
	if s.app == nil || s.app.QueryEngine == nil || !s.app.QueryEngine.Available() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": map[string]interface{}{
				"code":      "provider_unavailable",
				"message":   "No language model provider is available",
				"retryable": true,
			},
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx, cancel := context.WithCancel(req.Context())
	var heartbeatWG sync.WaitGroup
	defer func() {
		cancel()
		heartbeatWG.Wait()
	}()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	var writerMu sync.Mutex
	writeSSE := func(payload string) {
		writerMu.Lock()
		defer writerMu.Unlock()
		fmt.Fprint(w, payload)
		flusher.Flush()
	}

	send := func(event ChatEvent) {
		data, _ := json.Marshal(event)
		writeSSE(fmt.Sprintf("data: %s\n\n", data))
	}

	heartbeat := time.NewTicker(HeartbeatInterval)
	defer heartbeat.Stop()

	heartbeatWG.Add(1)
	go func() {
		defer heartbeatWG.Done()
		for {
			select {
			case <-heartbeat.C:
				writeSSE(": ping\n\n")
			case <-ctx.Done():
				return
			}
		}
	}()

	sendProgress := func(stage string, detail map[string]interface{}) {
		evt := map[string]interface{}{"type": "progress", "stage": stage}
		for k, v := range detail {
			evt[k] = v
		}
		data, _ := json.Marshal(evt)
		writeSSE(fmt.Sprintf("data: %s\n\n", data))
	}

	userID := userIDFromContext(req.Context())
	var sessionID string
	appInst := s.app.NewRequest(&app.AppOptions{
		Model:           chatReq.Model,
		OnTextDelta:     func(text string) { send(ChatEvent{Type: "text_delta", Content: text}) },
		OnThinkingDelta: func(text string) { send(ChatEvent{Type: "thinking_delta", Content: text}) },
		OnToolCall: func(name string, input map[string]interface{}) {
			send(ChatEvent{Type: "tool_call", Name: name, Input: input})
		},
		OnToolResult: func(name string, result string) { send(ChatEvent{Type: "tool_result", Name: name, Result: result}) },
		OnDiagnosisRecord: func(sessionID, dimension string, score int, question string) {
			s.recordDiagnosis(userID, sessionID, dimension, score, question)
		},
		OnInterviewQuestions: func(questions []string) {
			s.saveInterviewQuestions(sessionID, questions)
		},
		OnRetry: func(attempt int, maxRetries int, reason string) {
			sendProgress("retrying", map[string]interface{}{
				"attempt":    attempt,
				"maxRetries": maxRetries,
				"reason":     reason,
				"step":       "model_retrying",
			})
		},
		OnProgress: sendProgress,
	})

	if chatReq.SessionID != "" {
		if s.canAccessSession(req, chatReq.SessionID, userID) {
			sessionID = chatReq.SessionID
		} else {
			send(ChatEvent{Type: "error", Error: "session access denied"})
			return
		}
	}
	if sessionID == "" {
		if c, err := req.Cookie("offerpilot_sid"); err == nil && c.Value != "" {
			if s.canAccessSession(req, c.Value, userID) {
				sessionID = c.Value
			}
		}
	}
	if sessionID == "" {
		newSession := appInst.SessionManager.Create(userID)
		sessionID = newSession.ID
	}

	http.SetCookie(w, &http.Cookie{Name: "offerpilot_sid", Value: sessionID, Path: "/", HttpOnly: false, SameSite: http.SameSiteLaxMode, MaxAge: 86400 * 30})

	send(ChatEvent{Type: "session", SessionID: sessionID})

	// 加载第二层画像（知识点 + 求职信息）到内存缓存
	if userID != "" {
		s.loadProfile(userID)
	}

	_, err := appInst.Agent.Run(ctx, sessionID, userID, chatReq.Message)
	if err != nil {
		send(ChatEvent{Type: "error", Error: err.Error()})
		return
	}

	// 每轮对话后，把规则提取到的求职信息同步到 user_profiles
	if userID != "" {
		s.saveUserProfileFromMemory(userID)
	}

	usage := appInst.Agent.GetUsage()
	send(ChatEvent{
		Type: "done",
		Usage: map[string]int{
			"inputTokens":  usage.InputTokens,
			"outputTokens": usage.OutputTokens,
			"totalTokens":  usage.TotalTokens,
			"iterations":   usage.Iterations,
		},
	})

	writeSSE("data: [DONE]\n\n")
}

// handleSessionNew 创建全新的对话会话（新会话功能入口）。
// 若存在旧会话，先生成摘要写入 MemoryEntry（context 类型），再标记旧会话为 completed。
func (s *Server) handleSessionNew(w http.ResponseWriter, req *http.Request) {
	s.cors(w)

	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := userIDFromContext(req.Context())

	// 1. 若有旧会话，生成摘要并写入长期记忆
	if c, err := req.Cookie("offerpilot_sid"); err == nil && c.Value != "" && s.canAccessSession(req, c.Value, userID) {
		if oldSess, err := s.app.SessionManager.Get(c.Value); err == nil && oldSess != nil && len(oldSess.Messages) > 0 {
			s.summarizeAndRemember(userID, c.Value, oldSess)
			// 标记旧会话为 completed
			s.app.SessionManager.Transition(c.Value, session.SessionStateCompleted)
		}
	}

	// 2. 创建新会话
	session := s.app.SessionManager.Create(userID)

	http.SetCookie(w, &http.Cookie{
		Name:     "offerpilot_sid",
		Value:    session.ID,
		Path:     "/",
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 30,
	})

	writeJSON(w, http.StatusOK, map[string]string{"sessionId": session.ID})
}

// summarizeAndRemember 从旧会话消息中提取关键信息，写入长期记忆。
func (s *Server) summarizeAndRemember(userID, sessionID string, oldSess *session.Session) {
	if s.app == nil || s.app.MemoryStore == nil {
		return
	}
	ms := s.app.MemoryStore

	// 提取用户消息和诊断维度
	var userMessages []string
	dimensions := make(map[string]bool)
	for _, msg := range oldSess.Messages {
		if msg.Role == "user" && msg.Content != "" {
			userMessages = append(userMessages, msg.Content)
		}
	}

	// 从诊断记录中提取维度
	if s.mysqlDB != nil {
		rows, err := s.mysqlDB.Query(
			`SELECT DISTINCT dimension FROM diagnoses WHERE session_id = ?`,
			sessionID,
		)
		if err == nil {
			for rows.Next() {
				var dim string
				if rows.Scan(&dim) == nil {
					dimensions[dim] = true
				}
			}
			rows.Close()
		}
	}

	// 生成会话摘要
	topics := extractTopicsFromMessages(userMessages)
	summary := buildSessionSummary(topics, dimensions, len(oldSess.Messages))

	// 写入 MemoryEntry（context 类型，跨会话保留）
	ms.Add(memory.MemoryEntry{
		UserID:     userID,
		SessionID:  sessionID,
		Type:       memory.MemoryTypeContext,
		Content:    summary,
		Importance: 0.8,
	})
}

// extractTopicsFromMessages 从用户消息中提取关键主题。
func extractTopicsFromMessages(messages []string) []string {
	keywords := []string{
		"Agent", "RAG", "LLM", "embedding", "向量", "ReAct", "Tool Call",
		"Prompt", "微调", "训练", "推理", "大模型", "GPT", "Claude", "LangChain",
		"Docker", "Kubernetes", "K8s", "微服务", "架构", "分布式", "高并发",
		"Python", "Go", "Java", "TypeScript", "Rust",
		"PostgreSQL", "MySQL", "Redis", "MongoDB", "Elasticsearch", "Milvus",
		"评测", "benchmark", "延迟", "QPS", "吞吐", "性能",
		"Context Window", "Token", "Chunk", "Rerank", "HyDE",
		"System Prompt", "Memory", "Session", "Hook", "Permission",
		"STAR", "简历", "面试", "JD", "offer",
	}
	seen := make(map[string]bool)
	var topics []string
	for _, msg := range messages {
		lower := strings.ToLower(msg)
		for _, kw := range keywords {
			kwLower := strings.ToLower(kw)
			if strings.Contains(lower, kwLower) && !seen[kw] {
				seen[kw] = true
				topics = append(topics, kw)
			}
		}
	}
	return topics
}

// buildSessionSummary 构建会话摘要文本。
func buildSessionSummary(topics []string, dimensions map[string]bool, msgCount int) string {
	var parts []string
	if len(topics) > 0 {
		parts = append(parts, "讨论主题："+strings.Join(topics, "、"))
	}
	if len(dimensions) > 0 {
		var dims []string
		for d := range dimensions {
			dims = append(dims, d)
		}
		parts = append(parts, "诊断维度："+strings.Join(dims, "、"))
	}
	parts = append(parts, fmt.Sprintf("共 %d 轮对话", msgCount/2))
	return strings.Join(parts, " | ")
}

// handleSessions 返回当前用户的所有会话列表。
func (s *Server) handleSessions(w http.ResponseWriter, req *http.Request) {
	s.cors(w)

	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := userIDFromContext(req.Context())
	sessions := s.app.SessionManager.ListByUserID(userID)
	if userID == "" {
		sessions = nil
		if c, err := req.Cookie("offerpilot_sid"); err == nil && s.canAccessSession(req, c.Value, "") {
			if sess, err := s.app.SessionManager.Get(c.Value); err == nil {
				sessions = []*session.Session{sess}
			}
		}
	}

	type sessionItem struct {
		ID        string `json:"id"`
		UpdatedAt int64  `json:"updatedAt"`
	}
	items := make([]sessionItem, 0, len(sessions))
	for _, sess := range sessions {
		// 只返回有消息的会话，过滤空会话
		if len(sess.Messages) > 0 {
			items = append(items, sessionItem{
				ID:        sess.ID,
				UpdatedAt: sess.UpdatedAt,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"sessions": items})
}

func (s *Server) handleSession(w http.ResponseWriter, req *http.Request) {
	s.cors(w)

	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if !s.validateAuth(req) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	switch req.Method {
	case http.MethodPost:
		userID := userIDFromContext(req.Context())
		// Try to reuse existing session from cookie first
		var sessionID string
		var existingMsgs []*schema.Message
		if c, err := req.Cookie("offerpilot_sid"); err == nil && c.Value != "" {
			if s.canAccessSession(req, c.Value, userID) {
				sessionID = c.Value
				existingMsgs, _ = s.app.SessionManager.GetMessages(c.Value)
			}
		}
		if sessionID == "" {
			session := s.app.SessionManager.Create(userID)
			sessionID = session.ID
		}
		fmt.Printf("[DEBUG] handleSession POST sid=%s\n", sessionID)
		http.SetCookie(w, &http.Cookie{Name: "offerpilot_sid", Value: sessionID, Path: "/", HttpOnly: false, SameSite: http.SameSiteLaxMode, MaxAge: 86400 * 30})
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{"sessionId": sessionID}
		if len(existingMsgs) > 0 {
			type mj struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}
			list := make([]mj, 0, len(existingMsgs))
			for _, m := range existingMsgs {
				if m.Content != "" && (m.Role == "user" || m.Role == "assistant") {
					list = append(list, mj{Role: string(m.Role), Content: m.Content})
				}
			}
			if len(list) > 0 {
				resp["messages"] = list
			}
		}
		json.NewEncoder(w).Encode(resp)
	case http.MethodGet:
		sid := req.URL.Query().Get("id")
		if sid == "" {
			// Fallback: try cookie
			if c, err := req.Cookie("offerpilot_sid"); err == nil && c.Value != "" {
				sid = c.Value
			}
		}
		if sid == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		userID := userIDFromContext(req.Context())
		if !s.canAccessSession(req, sid, userID) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		sess, err := s.app.SessionManager.Get(sid)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		msgs, _ := s.app.SessionManager.GetMessages(sid)
		out := make([]map[string]interface{}, 0, len(msgs))
		for _, m := range msgs {
			out = append(out, map[string]interface{}{
				"role":    string(m.Role),
				"content": m.Content,
			})
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":        sess.ID,
			"messages":  out,
			"createdAt": sess.CreatedAt,
			"updatedAt": sess.UpdatedAt,
		})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	case http.MethodDelete:
		sid := req.URL.Query().Get("id")
		if sid == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		userID := userIDFromContext(req.Context())
		if !s.canAccessSession(req, sid, userID) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := s.app.SessionManager.Delete(sid, userID); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	}
}

// recordDiagnosis delegates diagnosis orchestration while preserving the
// non-blocking behavior expected by chat and interview flows.
func (s *Server) recordDiagnosis(userID, sessionID, dimension string, score int, question string) {
	if _, err := s.diagnosisService().Record(
		context.Background(), userID, sessionID, dimension, score, question,
	); err != nil {
		logger.DefaultLogger.Warn("record diagnosis failed", map[string]interface{}{"error": err.Error()})
	}
}

func (s *Server) projectDiagnosis(_ context.Context, record DiagnosisRecord) error {
	s.upsertDiagnosisMemory(record.UserID, record.SessionID, record.Dimension, record.Score)
	s.updateActiveProfile(record.SessionID, record.Dimension, record.Question)
	s.saveKnowledgePoint(record.UserID, record.Dimension, record.Score)
	return nil
}

// saveInterviewQuestions 把 mock_interview 生成的题目序列存到活性画像。
func (s *Server) saveInterviewQuestions(sessionID string, questions []string) {
	if s.app == nil || s.app.MemoryStore == nil {
		return
	}
	ap := s.app.MemoryStore.GetActiveProfile(sessionID)
	ap.Questions = questions
	ap.QuestionIndex = 0 // 重置题号，准备从第 1 题开始
	s.app.MemoryStore.UpdateActiveProfile(sessionID, *ap)
}

// updateActiveProfile 更新第一层活性画像（面试状态机）。
func (s *Server) updateActiveProfile(sessionID, dimension, question string) {
	if s.app == nil || s.app.MemoryStore == nil {
		return
	}
	ap := s.app.MemoryStore.GetActiveProfile(sessionID)
	ap.CurrentTopic = dimension
	ap.CurrentQuestion = question
	ap.QuestionIndex++
	s.app.MemoryStore.UpdateActiveProfile(sessionID, *ap)
}

// saveKnowledgePoint 更新第二层知识点掌握情况（MySQL + 内存缓存）。
func (s *Server) saveKnowledgePoint(userID, pointName string, score int) {
	mastered := score >= 6
	now := time.Now().UnixMilli()

	// 更新内存缓存
	if s.app != nil && s.app.MemoryStore != nil {
		points := s.app.MemoryStore.GetKnowledgePoints(userID)
		found := false
		for i := range points {
			if points[i].UserID == userID && points[i].PointName == pointName {
				points[i].Score = score
				points[i].Mastered = mastered
				points[i].UpdatedAt = now
				found = true
				break
			}
		}
		if !found {
			points = append(points, memory.KnowledgePoint{
				UserID: userID, PointName: pointName, Score: score, Mastered: mastered, UpdatedAt: now,
			})
		}
		s.app.MemoryStore.SetKnowledgePoints(userID, points)
	}

	// 写 MySQL
	if s.mysqlDB != nil {
		if _, err := s.mysqlDB.Exec(
			`INSERT INTO knowledge_points (user_id, point_name, score, mastered, updated_at) VALUES (?,?,?,?,?)
			 ON DUPLICATE KEY UPDATE score=VALUES(score), mastered=VALUES(mastered), updated_at=VALUES(updated_at)`,
			userID, pointName, score, mastered, now,
		); err != nil {
			logger.DefaultLogger.Warn("save knowledge point failed", map[string]interface{}{"error": err.Error()})
		}
	}
}

// loadProfile 从 MySQL 加载第二层画像（知识点 + 求职信息）到内存缓存。
func (s *Server) loadProfile(userID string) {
	if s.mysqlDB == nil || s.app == nil || s.app.MemoryStore == nil {
		return
	}
	ms := s.app.MemoryStore

	// 加载知识点
	if rows, err := s.mysqlDB.Query(
		`SELECT user_id, point_name, score, mastered, updated_at FROM knowledge_points WHERE user_id = ? ORDER BY updated_at DESC`,
		userID,
	); err == nil {
		var points []memory.KnowledgePoint
		for rows.Next() {
			var p memory.KnowledgePoint
			var mastered int
			if rows.Scan(&p.UserID, &p.PointName, &p.Score, &mastered, &p.UpdatedAt) == nil {
				p.Mastered = mastered == 1
				points = append(points, p)
			}
		}
		rows.Close()
		ms.SetKnowledgePoints(userID, points)
	}

	// 加载求职信息
	var p memory.StructuredProfile
	var dir, pos, sit sql.NullString
	err := s.mysqlDB.QueryRow(
		`SELECT job_direction, target_position, current_situation, updated_at FROM user_profiles WHERE user_id = ?`,
		userID,
	).Scan(&dir, &pos, &sit, &p.UpdatedAt)
	if err == nil {
		p.UserID = userID
		p.JobDirection = dir.String
		p.TargetPosition = pos.String
		p.CurrentSituation = sit.String
		ms.SetProfile(userID, &p)
	}

	// 加载 MemoryEntry（weakness/strength/face/preference/context）到内存缓存
	if err := ms.LoadFromMySQL(userID); err != nil {
		logger.DefaultLogger.Warn("load memories failed", map[string]interface{}{"error": err.Error()})
	}
}

// saveUserProfileFromMemory 从内存记忆（face/preference）提取求职信息，写 user_profiles。
func (s *Server) saveUserProfileFromMemory(userID string) {
	if s.app == nil || s.app.MemoryStore == nil || s.mysqlDB == nil {
		return
	}
	ms := s.app.MemoryStore

	var jobDirection, targetPosition, situation string
	all := ms.Query(memory.MemoryQuery{UserID: userID})
	for _, m := range all {
		switch {
		case strings.Contains(m.Content, "求职方向"):
			jobDirection = strings.TrimPrefix(m.Content, "求职方向: ")
		case strings.Contains(m.Content, "求职目标"):
			targetPosition = strings.TrimPrefix(m.Content, "求职目标: ")
		case strings.Contains(m.Content, "技术栈"),
			strings.Contains(m.Content, "工作年限"),
			strings.Contains(m.Content, "自我介绍"),
			strings.Contains(m.Content, "姓名"):
			if situation != "" {
				situation += "；"
			}
			situation += m.Content
		}
	}

	now := time.Now().UnixMilli()
	if _, err := s.mysqlDB.Exec(
		`INSERT INTO user_profiles (user_id, job_direction, target_position, current_situation, updated_at) VALUES (?,?,?,?,?)
		 ON DUPLICATE KEY UPDATE job_direction=VALUES(job_direction), target_position=VALUES(target_position), current_situation=VALUES(current_situation), updated_at=VALUES(updated_at)`,
		userID, jobDirection, targetPosition, situation, now,
	); err != nil {
		logger.DefaultLogger.Warn("save user profile failed", map[string]interface{}{"error": err.Error()})
	}
}

// upsertDiagnosisMemory 根据诊断得分规则化更新用户记忆（弱项/强项）。
func (s *Server) upsertDiagnosisMemory(userID, sessionID, dimension string, score int) {
	if s.app == nil || s.app.MemoryStore == nil {
		return
	}
	if score < 6 {
		s.upsertMemory(userID, sessionID, memory.MemoryTypeWeakness, dimension, score)
	} else if score >= 7 {
		s.upsertMemory(userID, sessionID, memory.MemoryTypeStrength, dimension, score)
	}
}

// upsertMemory 按 user + type + 维度去重后写入一条记忆。
func (s *Server) upsertMemory(userID, sessionID string, mtype memory.MemoryType, dimension string, score int) {
	ms := s.app.MemoryStore
	query := memory.MemoryQuery{UserID: userID, Type: mtype}
	if userID == "" {
		query.SessionID = sessionID
	}
	existing := ms.Query(query)
	for _, e := range existing {
		if strings.Contains(e.Content, dimension) {
			return // 已有同维度记忆，跳过
		}
	}
	ms.Add(memory.MemoryEntry{
		UserID:     userID,
		SessionID:  sessionID,
		Type:       mtype,
		Content:    fmt.Sprintf("%s 维度得分 %d", dimension, score),
		Importance: 1.0,
	})
}

// loadDiagnoses loads persisted records through the diagnosis service.
func (s *Server) loadDiagnoses() {
	count, err := s.diagnosisService().Load(context.Background())
	if err != nil {
		logger.DefaultLogger.Warn("load diagnoses failed", map[string]interface{}{"error": err.Error()})
		return
	}
	if count > 0 {
		logger.DefaultLogger.Info("diagnoses loaded from MySQL", map[string]interface{}{"count": count})
	}
}

func (s *Server) handleDiagnosis(w http.ResponseWriter, req *http.Request) {
	s.cors(w)

	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if !s.validateAuth(req) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"})
		return
	}

	if req.Method == http.MethodGet {
		s.handleDiagnosisGet(w, req)
		return
	}

	if req.Method == http.MethodPost {
		s.handleDiagnosisPost(w, req)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleDiagnosisGet(w http.ResponseWriter, req *http.Request) {
	userID := userIDFromContext(req.Context())
	sessionID := ""
	if userID == "" {
		if cookie, err := req.Cookie("offerpilot_sid"); err == nil {
			sessionID = cookie.Value
		}
	}
	records, reviewStates := s.diagnosisState().Snapshot(userID, sessionID)

	dimensions := []string{"architecture", "engineering", "model", "rag", "multi-agent", "evaluation", "full-stack"}

	type DimScore struct {
		Dimension string `json:"dimension"`
		Score     int    `json:"score"`
		Count     int    `json:"count"`
	}

	dimensionScores := make([]DimScore, 0, len(dimensions))
	for _, dim := range dimensions {
		var sum int
		var count int
		for _, r := range records {
			if r.Dimension == dim {
				sum += r.Score
				count++
			}
		}
		avg := 0
		if count > 0 {
			avg = sum / count
		}
		dimensionScores = append(dimensionScores, DimScore{Dimension: dim, Score: avg, Count: count})
	}

	totalAnswered := len(records)
	avgScore := 0
	if totalAnswered > 0 {
		sum := 0
		for _, r := range records {
			sum += r.Score
		}
		avgScore = sum / totalAnswered
	}

	weakDimensions := make([]string, 0)
	for _, d := range dimensionScores {
		if d.Count > 0 && d.Score < 6 {
			weakDimensions = append(weakDimensions, d.Dimension)
		}
	}

	recent := make([]DiagnosisRecord, 0)
	if len(records) > 0 {
		start := len(records) - 10
		if start < 0 {
			start = 0
		}
		recent = records[start:]
	}

	type ReviewPriority struct {
		Dimension       string `json:"dimension"`
		Urgency         int    `json:"urgency"`
		DaysUntilReview *int   `json:"daysUntilReview"`
	}

	reviewPriority := make([]ReviewPriority, 0)
	now := time.Now().UnixMilli()
	for _, dim := range dimensions {
		state, ok := reviewStates[dim]
		if !ok {
			continue
		}
		daysUntil := int((state.NextReview - now) / (24 * 60 * 60 * 1000))
		urgency := 0
		if daysUntil <= 0 {
			urgency = 10
		} else {
			urgency = max(0, 5-daysUntil)
		}
		if urgency > 0 || daysUntil >= 0 {
			reviewPriority = append(reviewPriority, ReviewPriority{
				Dimension:       dim,
				Urgency:         urgency,
				DaysUntilReview: &daysUntil,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"dimensionScores": dimensionScores,
		"totalAnswered":   totalAnswered,
		"avgScore":        avgScore,
		"weakDimensions":  weakDimensions,
		"recent":          recent,
		"reviewPriority":  reviewPriority,
	})
}

func (s *Server) handleDiagnosisPost(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Dimension string `json:"dimension"`
		Score     int    `json:"score"`
		Question  string `json:"question"`
		SessionID string `json:"sessionId"`
	}

	if !decodeJSON(w, req, &body) {
		return
	}

	if body.Dimension == "" || body.Score == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dimension and score are required"})
		return
	}

	userID := userIDFromContext(req.Context())
	if body.SessionID == "" {
		if cookie, err := req.Cookie("offerpilot_sid"); err == nil {
			body.SessionID = cookie.Value
		}
	}
	if !s.canAccessSession(req, body.SessionID, userID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "session access denied"})
		return
	}
	s.recordDiagnosis(userID, body.SessionID, body.Dimension, body.Score, body.Question)

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleParsePDF(w http.ResponseWriter, req *http.Request) {
	s.cors(w)

	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.validateAuth(req) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"})
		return
	}

	// Recover from any panic in the parser so the connection survives
	// a malformed/encrypted/corrupt file instead of being torn down.
	defer func() {
		if r := recover(); r != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("Failed to parse file: %v", r),
			})
		}
	}()

	if err := req.ParseMultipartForm(10 << 20); err != nil {
		if isRequestTooLarge(err) {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "request_too_large", "Request body is too large", false)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to parse form"})
		return
	}

	file, handler, err := req.FormFile("file")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "No file provided"})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to read file"})
		return
	}

	filename := strings.ToLower(handler.Filename)

	switch {
	case strings.HasSuffix(filename, ".pdf"):
		text, pages, err := extractTextFromPDF(data)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "PDF parse failed: " + err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"text": text, "pages": pages, "format": "pdf"})

	case strings.HasSuffix(filename, ".docx"):
		text, err := extractTextFromDocx(data)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "DOCX parse failed: " + err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"text": text, "pages": nil, "format": "docx"})

	case strings.HasSuffix(filename, ".tex"):
		text := stripLatex(string(data))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"text": text, "pages": nil, "format": "tex"})

	case strings.HasSuffix(filename, ".txt"), strings.HasSuffix(filename, ".md"):
		text := string(data)
		format := "txt"
		if strings.HasSuffix(filename, ".md") {
			format = "md"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"text": text, "pages": nil, "format": format})

	default:
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Unsupported file format"})
	}
}

// extractTextFromPDF uses ledongthuc/pdf to pull real text from each page.
// It returns the joined text and the page count. Failures (encrypted,
// malformed, empty) are reported via error so the caller can answer 400.
func extractTextFromPDF(data []byte) (string, int, error) {
	if len(data) == 0 {
		return "", 0, fmt.Errorf("empty file")
	}

	reader, err := pdfreader.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", 0, fmt.Errorf("open pdf: %w", err)
	}

	totalPages := reader.NumPage()
	var b strings.Builder
	for i := 1; i <= totalPages; i++ {
		page := reader.Page(i)
		text, err := page.GetPlainText(nil)
		if err != nil {
			return "", totalPages, fmt.Errorf("read page %d: %w", i, err)
		}
		b.WriteString(text)
		b.WriteString("\n\n")
	}

	text := normalizeExtractedText(b.String())
	if strings.TrimSpace(text) == "" {
		return "", totalPages, fmt.Errorf("no extractable text (scanned/encrypted PDF?)")
	}
	return text, totalPages, nil
}

// extractTextFromDocx reads word/document.xml from the docx zip and joins
// all <w:t> runs in document order. Headings become their own paragraphs.
func extractTextFromDocx(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("open docx zip: %w", err)
	}

	var docFile *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			docFile = f
			break
		}
	}
	if docFile == nil {
		return "", fmt.Errorf("word/document.xml not found")
	}

	rc, err := docFile.Open()
	if err != nil {
		return "", fmt.Errorf("open document.xml: %w", err)
	}
	defer rc.Close()

	dec := xml.NewDecoder(rc)
	var (
		out       strings.Builder
		inP       bool
		inT       bool
		paraStart = false
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("decode xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				inP = true
				paraStart = true
			case "t":
				if inP {
					inT = true
				}
			}
		case xml.CharData:
			if inT {
				if paraStart && out.Len() > 0 {
					out.WriteString("\n")
				}
				paraStart = false
				out.WriteString(string(t))
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inT = false
			case "p":
				inP = false
				out.WriteString("\n")
			}
		}
	}

	text := normalizeExtractedText(out.String())
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("no extractable text in docx")
	}
	return text, nil
}

func normalizeExtractedText(text string) string {
	// Collapse runs of blank lines that PDFs often emit between pages.
	text = regexp.MustCompile(`[ \t]+\n`).ReplaceAllString(text, "\n")
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func stripLatex(tex string) string {
	text := regexp.MustCompile(`%.+`).ReplaceAllString(tex, "")
	text = strings.ReplaceAll(text, "\\begin{document}", "")
	text = strings.ReplaceAll(text, "\\end{document}", "")
	text = regexp.MustCompile(`\\(?:documentclass|usepackage|pagestyle|geometry|setlength|renewcommand|newcommand)\{[^}]*\}(?:\[[^\]]*\])?(?:\{[^}]*\})*`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`\\(?:section|subsection|subsubsection|textbf|textit|emph|underline|href)\{([^}]*)\}`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`\\(?:begin|end)\{[^}]*\}`).ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\\item ", "- ")
	text = regexp.MustCompile(`\\[a-zA-Z]+\*?(?:\[[^\]]*\])?(?:\{([^}]*)\})?`).ReplaceAllString(text, "$1")
	text = strings.ReplaceAll(text, "{", "")
	text = strings.ReplaceAll(text, "}", "")
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func (s *Server) handleParseURL(w http.ResponseWriter, req *http.Request) {
	s.cors(w)

	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.validateAuth(req) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"})
		return
	}

	var body struct {
		URL string `json:"url"`
	}

	if !decodeJSON(w, req, &body) {
		return
	}

	rawURL := strings.TrimSpace(body.URL)
	if rawURL == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "URL is required"})
		return
	}

	targetURL, err := parsePublicHTTPURL(rawURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	client := newSafeHTTPClient()
	resp, err := client.Get(targetURL.String())
	if err != nil {
		if strings.Contains(err.Error(), "timeout") {
			w.WriteHeader(http.StatusRequestTimeout)
			json.NewEncoder(w).Encode(map[string]string{"error": "URL request timed out (10s)"})
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "URL fetch failed: " + err.Error()})
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to fetch URL (%d)", resp.StatusCode)})
		return
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if contentType != "" && !strings.HasPrefix(contentType, "text/html") &&
		!strings.HasPrefix(contentType, "text/plain") && !strings.HasPrefix(contentType, "application/xhtml+xml") {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "URL must return HTML or plain text"})
		return
	}

	const maxFetchedPageBytes = 4 << 20
	if resp.ContentLength > maxFetchedPageBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "URL content is too large"})
		return
	}
	htmlData, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchedPageBytes+1))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to read URL content"})
		return
	}
	if len(htmlData) > maxFetchedPageBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "URL content is too large"})
		return
	}

	text := extractTextFromHtml(string(htmlData))
	if strings.TrimSpace(text) == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "No text content found at URL"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"text": text, "source": targetURL.String()})
}

func parsePublicHTTPURL(raw string) (*neturl.URL, error) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	target, err := neturl.Parse(raw)
	if err != nil || target.Hostname() == "" {
		return nil, fmt.Errorf("invalid URL")
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, fmt.Errorf("only HTTP and HTTPS URLs are allowed")
	}
	if target.User != nil {
		return nil, fmt.Errorf("URL credentials are not allowed")
	}
	if port := target.Port(); port != "" && port != "80" && port != "443" {
		return nil, fmt.Errorf("only ports 80 and 443 are allowed")
	}
	return target, nil
}

func isPublicIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsInterfaceLocalMulticast() &&
		!ip.IsMulticast()
}

func resolvePublicIPs(ctx context.Context, hostname string) ([]net.IP, error) {
	if ip := net.ParseIP(hostname); ip != nil {
		if !isPublicIP(ip) {
			return nil, fmt.Errorf("private or local addresses are not allowed")
		}
		return []net.IP{ip}, nil
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return nil, fmt.Errorf("URL host lookup failed: %w", err)
	}
	public := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		if !isPublicIP(address.IP) {
			return nil, fmt.Errorf("private or local addresses are not allowed")
		}
		public = append(public, address.IP)
	}
	if len(public) == 0 {
		return nil, fmt.Errorf("URL host has no public address")
	}
	return public, nil
}

func newSafeHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := resolvePublicIPs(ctx, host)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, ip := range ips {
				conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if dialErr == nil {
					return conn, nil
				}
				lastErr = dialErr
			}
			return nil, lastErr
		},
		TLSHandshakeTimeout: 5 * time.Second,
	}
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			_, err := parsePublicHTTPURL(req.URL.String())
			if err != nil {
				return err
			}
			_, err = resolvePublicIPs(req.Context(), req.URL.Hostname())
			return err
		},
	}
}

func extractTextFromHtml(htmlContent string) string {
	text := htmlContent
	text = regexp.MustCompile(`<script[\s\S]*?<\/script>`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`<style[\s\S]*?<\/style>`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`<nav[\s\S]*?<\/nav>`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`<footer[\s\S]*?<\/footer>`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`<header[\s\S]*?<\/header>`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`<!--[\s\S]*?-->`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`<br\s*\/?>`).ReplaceAllString(text, "\n")
	text = regexp.MustCompile(`<\/(?:p|div|h[1-6]|li|tr|section|article)>`).ReplaceAllString(text, "\n")
	text = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(text, " ")
	text = html.UnescapeString(text)
	text = regexp.MustCompile(`[ \t]+`).ReplaceAllString(text, " ")
	text = regexp.MustCompile(`\n[ \t]+`).ReplaceAllString(text, "\n")
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func (s *Server) handleStaticFile(w http.ResponseWriter, req *http.Request) {
	filePath := req.URL.Path
	if strings.HasPrefix(filePath, "/api/") {
		filePath = strings.TrimPrefix(filePath, "/api/")
	}
	filePath = strings.TrimPrefix(filePath, "/")
	filePath = "web/" + filePath
	data, err := webFiles.ReadFile(filePath)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	// Patch the Next.js page chunk to restore messages on load.
	// Two replacements:
	// 1. Save setMessages to window.__OP_SET_MSGS__ so it survives minification.
	// 2. After POST /api/session returns, check for embedded messages.
	if strings.Contains(filePath, "page-") && strings.HasSuffix(filePath, ".js") {
		data = patchPageChunk(data)
	}

	contentType := getContentType(filepath.Ext(filePath))
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// authScript 注入到 index.html，根据登录状态动态切换 header 中的登录按钮。
const authScript = `<script>
(function(){
  var user=null;
  function apply(){
    var el=document.getElementById('auth-link');
    if(!el) return;
    if(user && user.username){
      el.textContent=user.username;
      el.title='已登录';
    }else{
      el.textContent='登录';
      el.title='';
    }
  }
  fetch('/api/me',{credentials:'include'}).then(function(r){if(r.ok)return r.json();return null;}).then(function(d){if(d&&d.username){user=d;}apply();}).catch(function(){});
  setInterval(apply,2000);
})();
</script>`

// injectAuthScript 在 index.html 的 </body> 前注入登录状态脚本。
func injectAuthScript(data []byte) []byte {
	return bytes.Replace(data, []byte("</body>"), []byte(authScript+"</body>"), 1)
}

func (s *Server) handleStaticOrSPA(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/" {
		data, err := webFiles.ReadFile("web/index.html")
		if err != nil {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(injectAuthScript(data))
		return
	}

	filePath := path.Join("web", req.URL.Path)
	data, err := webFiles.ReadFile(filePath)
	if err != nil {
		data, err = webFiles.ReadFile("web/index.html")
		if err != nil {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(injectAuthScript(data))
		return
	}

	contentType := getContentType(filepath.Ext(filePath))
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// patchPageChunk patches the compiled Next.js page chunk so that the
// React app loads existing session messages on mount. Two patches:
//  1. Expose the setMessages setter as window.__OP_SET_MSGS__
//  2. After getting the sessionId from POST /api/session, also check for
//     a "messages" field and call setMessages with it.
func patchPageChunk(data []byte) []byte {
	// Patch 1: before "async function y(){" insert "window.__OP_SET_MSGS__=t;"
	data = bytes.Replace(data,
		[]byte("null);async function y(){"),
		[]byte("null);window.__OP_SET_MSGS__=t;async function y(){"),
		1)

	// Patch 2: after "x(t.sessionId)" add messages restoration.
	// The inserted snippet checks t.messages (from the POST /api/session
	// JSON response) and calls the React setter with properly formatted
	// message objects.
	oldP2 := []byte("x(t.sessionId)}catch(e){x(\"")
	newP2 := []byte("x(t.sessionId);if(t.messages" +
		"&&t.messages.length)window.__OP_SET_MSGS__(" +
		"t.messages.map(function(m){return{id:(Date.now()+" +
		"Math.random()).toString(36),role:m.role,content:m.content}}))" +
		"}catch(e){x(\"")
	data = bytes.Replace(data, oldP2, newP2, 1)

	// Patch 3: make the scroll-to-bottom effect also depend on the active
	// view (h), so returning to the chat view re-scrolls to the latest
	// message instead of staying at the top.
	data = bytes.Replace(data,
		[]byte("scrollIntoView({behavior:\"smooth\"})},[e]"),
		[]byte("scrollIntoView({behavior:\"smooth\"})},[e,h]"),
		1)

	// Patch 4: make "new session" (onReset) call /api/session/new so it
	// starts a fresh chat instead of reusing the cookie session.
	data = bytes.Replace(data,
		[]byte("onReset:function(){t([]),y()}"),
		[]byte("onReset:function(){t([]),fetch(\"/api/session/new\",{method:\"POST\"}).then(function(e){return e.json()}).then(function(d){x(d.sessionId)})}"),
		1)

	// Patch 5: add a "登录" link next to the "新会话" button in the header.
	data = bytes.Replace(data,
		[]byte("新会话\"]})"),
		[]byte("新会话\"]}),(0,a.jsx)(\"a\",{id:\"auth-link\",href:\"/login\",style:{textDecoration:\"none\"},className:\"flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs text-slate-500 hover:bg-slate-100 hover:text-primary transition-all\",children:\"登录\"})"),
		1)

	return data
}

func getContentType(ext string) string {
	switch ext {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".txt":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

// handleUploadPage serves a small diagnostic page that exercises the
// /api/parse-pdf and /api/resume endpoints end-to-end. Useful when the
// Next.js frontend hasn't yet wired up the upload UI.
func (s *Server) handleUploadPage(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(uploadPageHTML))
}

func (s *Server) handleUploadJS(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(uploadPageJS))
}

const uploadPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8"/>
<title>简历上传调试 · OfferPilot</title>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<link rel="stylesheet" href="/style.css"/>
<style>
.upload-wrap{max-width:760px;margin:0 auto;padding:32px 24px;overflow:auto}
.upload-card{border:1px solid var(--line);border-radius:var(--radius);background:var(--card);padding:28px;box-shadow:var(--shadow)}
.upload-card h1{margin:0 0 8px;font-size:24px}
.upload-card p.sub{color:var(--text-soft);margin:0 0 20px;font-size:14px}
.drop-zone{border:2px dashed var(--line);border-radius:12px;padding:32px 16px;text-align:center;color:var(--text-soft);transition:all .15s;cursor:pointer;background:#fafaf9}
.drop-zone:hover,.drop-zone.drag{border-color:var(--accent);background:var(--accent-soft);color:var(--accent-strong)}
.drop-zone input{display:none}
.btn{padding:9px 18px;border-radius:10px;background:var(--accent);color:#fff;border:0;cursor:pointer;font-size:14px;font-weight:500;transition:background .15s}
.btn:hover{background:var(--accent-strong)}
.btn:disabled{background:var(--text-faint);cursor:not-allowed}
.btn.secondary{background:#fff;color:var(--text);border:1px solid var(--line)}
.btn.secondary:hover{border-color:var(--accent);color:var(--accent-strong)}
.row{display:flex;gap:10px;align-items:center;margin-top:16px;flex-wrap:wrap}
.meta{font-size:12.5px;color:var(--text-faint);margin-top:8px}
textarea#extracted{width:100%;min-height:240px;border:1px solid var(--line);border-radius:10px;padding:12px;font:13px ui-monospace,Consolas,monospace;resize:vertical;background:#fafaf9}
.section{margin-top:24px}
.section h3{margin:0 0 10px;font-size:15px}
.diag-item{border:1px solid var(--line);border-radius:10px;padding:14px;margin-bottom:10px;background:#fff}
.diag-item .head{display:flex;justify-content:space-between;align-items:center;margin-bottom:8px;font-weight:600}
.score-pill{padding:2px 10px;border-radius:999px;background:var(--accent-soft);color:var(--accent-strong);font-size:12px;font-weight:600}
.issues{color:#b91c1c;font-size:13px;margin:4px 0}
.suggestions{color:var(--text-soft);font-size:13px}
.err{color:#b91c1c;background:#fef2f2;border:1px solid #fecaca;padding:10px 12px;border-radius:8px;margin-top:12px;font-size:13px}
.toast{position:fixed;top:20px;right:20px;padding:10px 16px;border-radius:10px;background:#1c1917;color:#fff;font-size:13px;opacity:0;transform:translateY(-8px);transition:all .2s;z-index:50}
.toast.show{opacity:1;transform:none}
</style>
</head>
<body>
<div class="topbar"><div class="topbar-inner">
  <div class="brand"><div class="brand-mark">OP</div><div class="brand-name">OfferPilot · 简历上传调试</div></div>
  <div class="topbar-right"><a class="login-btn" href="/" style="text-decoration:none">返回主页</a></div>
</div></div>

<div class="upload-wrap">
  <div class="upload-card">
    <h1>上传简历 → 自动解析 → 诊断</h1>
    <p class="sub">支持 PDF / DOCX / TXT / MD / TEX。先解析得到文本，再调用 <code>/api/resume</code> 跑诊断。</p>

    <label class="drop-zone" id="dropZone">
      <input type="file" id="fileInput" accept=".pdf,.docx,.doc,.txt,.md,.tex"/>
      <div>📄 点击或拖拽简历到这里</div>
      <div class="meta">最大 10MB</div>
    </label>

    <div class="row">
      <button class="btn" id="parseBtn" disabled>1. 解析文件</button>
      <button class="btn secondary" id="diagnoseBtn" disabled>2. 跑诊断</button>
      <span class="meta" id="fileMeta"></span>
    </div>

    <div id="parseErr" class="err hidden"></div>

    <div class="section">
      <h3>提取的文本 <span class="meta" id="textMeta"></span></h3>
      <textarea id="extracted" placeholder="解析后会显示在这里..."></textarea>
    </div>

    <div class="section">
      <h3>诊断结果</h3>
      <div id="diagnosis"></div>
    </div>
  </div>
</div>

<div class="toast" id="toast"></div>
<script src="/upload.js"></script>
</body>
</html>`

const uploadPageJS = `
var fileInput = document.getElementById('fileInput');
var dropZone = document.getElementById('dropZone');
var parseBtn = document.getElementById('parseBtn');
var diagnoseBtn = document.getElementById('diagnoseBtn');
var fileMeta = document.getElementById('fileMeta');
var textMeta = document.getElementById('textMeta');
var extracted = document.getElementById('extracted');
var diagnosis = document.getElementById('diagnosis');
var parseErr = document.getElementById('parseErr');
var toast = document.getElementById('toast');
var currentText = '';

function toastMsg(msg){toast.textContent=msg;toast.classList.add('show');setTimeout(function(){toast.classList.remove('show');},1800);}

function setFile(f){
  if(!f) return;
  if(f.size>10*1024*1024){parseErr.textContent='文件超过 10MB';parseErr.classList.remove('hidden');return;}
  parseErr.classList.add('hidden');
  currentFile=f;
  fileMeta.textContent=f.name+' · '+Math.round(f.size/1024)+' KB';
  parseBtn.disabled=false;
  diagnoseBtn.disabled=true;
  extracted.value='';
  diagnosis.innerHTML='';
  textMeta.textContent='';
}

fileInput.addEventListener('change',function(e){setFile(e.target.files[0]);});
['dragenter','dragover'].forEach(function(ev){dropZone.addEventListener(ev,function(e){e.preventDefault();dropZone.classList.add('drag');});});
['dragleave','drop'].forEach(function(ev){dropZone.addEventListener(ev,function(e){e.preventDefault();dropZone.classList.remove('drag');});});
dropZone.addEventListener('drop',function(e){if(e.dataTransfer.files.length) setFile(e.dataTransfer.files[0]);});

parseBtn.addEventListener('click',async function(){
  if(!currentFile) return;
  parseBtn.disabled=true;parseBtn.textContent='解析中...';
  parseErr.classList.add('hidden');
  var fd=new FormData();fd.append('file',currentFile);
  try{
    var res=await fetch('/api/parse-pdf',{method:'POST',body:fd,credentials:'include'});
    var data=await res.json();
    if(!res.ok) throw new Error(data.error||('HTTP '+res.status));
    currentText=data.text||'';
    extracted.value=currentText;
    textMeta.textContent=(data.pages?' · '+data.pages+' 页':'')+' · '+data.format+' · '+currentText.length+' 字';
    toastMsg('解析成功');
    diagnoseBtn.disabled=!currentText.trim();
  }catch(e){
    parseErr.textContent='解析失败: '+e.message;
    parseErr.classList.remove('hidden');
  }finally{
    parseBtn.disabled=false;parseBtn.textContent='1. 解析文件';
  }
});

diagnoseBtn.addEventListener('click',async function(){
  if(!currentText.trim()) return;
  diagnoseBtn.disabled=true;diagnoseBtn.textContent='诊断中...';
  diagnosis.innerHTML='';
  try{
    var res=await fetch('/api/resume',{method:'POST',headers:{'Content-Type':'application/json'},credentials:'include',body:JSON.stringify({content:currentText})});
    var data=await res.json();
    if(!res.ok) throw new Error(data.error||('HTTP '+res.status));
    renderDiagnosis(data.diagnosis||[]);
    toastMsg('诊断完成');
  }catch(e){
    diagnosis.innerHTML='<div class="err">诊断失败: '+e.message+'</div>';
  }finally{
    diagnoseBtn.disabled=false;diagnoseBtn.textContent='2. 跑诊断';
  }
});

function renderDiagnosis(items){
  if(!items.length){diagnosis.innerHTML='<div class="meta">没有发现问题，干得漂亮 🎉</div>';return;}
  diagnosis.innerHTML=items.map(function(it){
    return '<div class="diag-item">'
      +'<div class="head"><span>'+(it.section||'(未命名段落)')+'</span><span class="score-pill">'+it.score+'/10</span></div>'
      +(it.issues&&it.issues.length?'<div class="issues">⚠ '+escapeHtml(it.issues.join(' · '))+'</div>':'')
      +'<div class="suggestions">💡 '+escapeHtml((it.suggestions||[]).join(' · '))+'</div>'
      +'</div>';
  }).join('');
}

function escapeHtml(s){return (s||'').replace(/[&<>\"']/g,function(c){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c];});}
`
