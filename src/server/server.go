package server

import (
	"archive/zip"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"MyOfferPilot/src/app"
	"MyOfferPilot/src/logger"
	"MyOfferPilot/src/realtime"

	"github.com/cloudwego/eino/schema"
	pdfreader "github.com/ledongthuc/pdf"
)

//go:embed web/*
var webFiles embed.FS

const (
	HeartbeatInterval = 15 * time.Second
)

type Server struct {
	port       string
	apiKey     string
	httpServer *http.Server
	app        *app.App
	mu         sync.Mutex
}

type ChatRequest struct {
	Message   string `json:"message"`
	SessionID string `json:"sessionId"`
	Model     string `json:"model"`
}

type ChatEvent struct {
	Type       string                 `json:"type"`
	Content    string                 `json:"content,omitempty"`
	Name       string                 `json:"name,omitempty"`
	Input      map[string]interface{} `json:"input,omitempty"`
	Result     string                 `json:"result,omitempty"`
	SessionID  string                 `json:"sessionId,omitempty"`
	Usage      map[string]int         `json:"usage,omitempty"`
	Error      string                 `json:"error,omitempty"`
}

func NewServer(port string) *Server {
	if port == "" {
		port = "3001"
	}
	return &Server{
		port:   port,
		apiKey: os.Getenv("OFFERPILOT_API_KEY"),
	}
}

func (s *Server) Start() error {
	s.mu.Lock()
	if s.app == nil {
		s.app = app.CreateApp(nil)
	}
	s.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/chat", s.handleChat)
	mux.HandleFunc("/api/session", s.handleSession)
	mux.HandleFunc("/api/transcribe", s.handleTranscribe)
	mux.HandleFunc("/api/tts", s.handleTTS)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/diagnosis", s.handleDiagnosis)
	mux.HandleFunc("/api/interview", s.handleInterview)
	mux.HandleFunc("/api/match", s.handleMatch)
	mux.HandleFunc("/api/resume", s.handleResume)
	mux.HandleFunc("/api/parse-pdf", s.handleParsePDF)
	mux.HandleFunc("/api/parse-url", s.handleParseURL)
	mux.HandleFunc("/api/_next/", s.handleStaticFile)
	mux.HandleFunc("/api/brand/", s.handleStaticFile)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/_next/", s.handleStaticFile)
	mux.HandleFunc("/brand/", s.handleStaticFile)
	mux.HandleFunc("/upload", s.handleUploadPage)
	mux.HandleFunc("/upload.js", s.handleUploadJS)
	mux.HandleFunc("/radar", s.handleRadar)
	mux.HandleFunc("/", s.handleStaticOrSPA)

	s.httpServer = &http.Server{
		Addr:    ":" + s.port,
		Handler: mux,
	}

	logger.DefaultLogger.Info("Server starting", map[string]interface{}{
		"port": s.port,
		"auth": s.apiKey != "",
	})

	return s.httpServer.ListenAndServe()
}

func (s *Server) Stop() error {
	logger.DefaultLogger.Info("Server stopping")
	return s.httpServer.Shutdown(context.Background())
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
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-File-Name")
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
	if err := json.NewDecoder(req.Body).Decode(&chatReq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return 
	}

	if chatReq.Message == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "message is required"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	send := func(event ChatEvent) {
		data, _ := json.Marshal(event)
		fmt.Fprintf(w, "data: %s\n\n", string(data))
		flusher.Flush()
	}

	heartbeat := time.NewTicker(HeartbeatInterval)
	defer heartbeat.Stop()

	go func() {
		for {
			select {
			case <-heartbeat.C:
				fmt.Fprintf(w, ": ping\n\n")
				flusher.Flush()
			case <-ctx.Done():
				return
			}
		}
	}()

	s.mu.Lock()
	appInst := app.CreateApp(&app.AppOptions{
		Model:           chatReq.Model,
		SessionManager:  s.app.SessionManager,
		MemoryStore:     s.app.MemoryStore,
		OnTextDelta:     func(text string) { send(ChatEvent{Type: "text_delta", Content: text}) },
		OnThinkingDelta: func(text string) { send(ChatEvent{Type: "thinking_delta", Content: text}) },
		OnToolCall:      func(name string, input map[string]interface{}) { send(ChatEvent{Type: "tool_call", Name: name, Input: input}) },
		OnToolResult:    func(name string, result string) { send(ChatEvent{Type: "tool_result", Name: name, Result: result}) },
		OnDiagnosisRecord: func(sessionID, dimension string, score int, question string) {
			recordDiagnosis(sessionID, dimension, score, question)
		},
	})
	s.mu.Unlock()

	var sessionID string
	if chatReq.SessionID != "" {
		_, err := appInst.SessionManager.Get(chatReq.SessionID)
		if err == nil {
			sessionID = chatReq.SessionID
		}
	}
	if sessionID == "" {
		if c, err := req.Cookie("offerpilot_sid"); err == nil && c.Value != "" {
			_, err := appInst.SessionManager.Get(c.Value)
			if err == nil {
				sessionID = c.Value
			}
		}
	}
	if sessionID == "" {
		newSession := appInst.SessionManager.Create("")
		sessionID = newSession.ID
	}

	http.SetCookie(w, &http.Cookie{Name: "offerpilot_sid", Value: sessionID, Path: "/", HttpOnly: false, SameSite: http.SameSiteLaxMode, MaxAge: 86400 * 30})

	send(ChatEvent{Type: "session", SessionID: sessionID})

	_, err := appInst.Agent.Run(ctx, sessionID, chatReq.Message)
	if err != nil {
		send(ChatEvent{Type: "error", Error: err.Error()})
		return
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

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
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
		// Try to reuse existing session from cookie first
		var sessionID string
		var existingMsgs []*schema.Message
		if c, err := req.Cookie("offerpilot_sid"); err == nil && c.Value != "" {
			if sess, err := s.app.SessionManager.Get(c.Value); err == nil {
				sessionID = c.Value
				existingMsgs, _ = s.app.SessionManager.GetMessages(c.Value)
				_ = sess
			}
		}
		if sessionID == "" {
			session := s.app.SessionManager.Create("")
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
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, req *http.Request) {
	s.cors(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleTranscribe(w http.ResponseWriter, req *http.Request) {
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

	audio, err := io.ReadAll(req.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to read audio"})
		return
	}

	if len(audio) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "audio body is required"})
		return
	}

	result, err := realtime.TranscribeAudio(realtime.TranscribeAudioInput{
		Audio:       audio,
		FileName:    req.Header.Get("X-File-Name"),
		ContentType: req.Header.Get("Content-Type"),
	})
	if err != nil {
		logger.DefaultLogger.Error("transcribe failed", map[string]interface{}{"error": err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleTTS(w http.ResponseWriter, req *http.Request) {
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

	var ttsReq struct {
		Text   string `json:"text"`
		Voice  string `json:"voice"`
		Format string `json:"format"`
	}

	if err := json.NewDecoder(req.Body).Decode(&ttsReq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}

	if ttsReq.Text == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "text is required"})
		return
	}

	result, err := realtime.SynthesizeSpeech(realtime.SynthesizeSpeechInput{
		Text:   ttsReq.Text,
		Voice:  ttsReq.Voice,
		Format: ttsReq.Format,
	})
	if err != nil {
		logger.DefaultLogger.Error("tts failed", map[string]interface{}{"error": err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write(result.Audio)
}

func (s *Server) handleConfig(w http.ResponseWriter, req *http.Request) {
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
		s.handleConfigGet(w)
		return
	}

	if req.Method == http.MethodPost {
		s.handleConfigPost(w, req)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleConfigGet(w http.ResponseWriter) {
	env := loadEnv()
	for _, v := range os.Environ() {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) == 2 && env[parts[0]] == "" {
			env[parts[0]] = parts[1]
		}
	}

	type ModelEntry struct {
		Name            string `json:"name"`
		Provider        string `json:"provider"`
		Model           string `json:"model"`
		BaseURL         string `json:"base_url"`
		EnvKey          string `json:"env_key"`
		ModelEnvKey     *string `json:"model_env_key"`
		BaseURLEnvKey   *string `json:"base_url_env_key"`
		Available       bool   `json:"available"`
	}

	textModels := []ModelEntry{
		{Name: "Claude", Provider: "anthropic", Model: env["ANTHROPIC_MODEL"], EnvKey: "ANTHROPIC_API_KEY", Available: env["ANTHROPIC_API_KEY"] != "" && env["ANTHROPIC_API_KEY"] != "sk-ant-..."},
		{Name: "OpenAI", Provider: "openai", Model: env["OPENAI_MODEL"], EnvKey: "OPENAI_API_KEY", Available: env["OPENAI_API_KEY"] != "" && env["OPENAI_API_KEY"] != "sk-..."},
		{Name: "DeepSeek", Provider: "deepseek", Model: env["DEEPSEEK_MODEL"], EnvKey: "DEEPSEEK_API_KEY", Available: env["DEEPSEEK_API_KEY"] != ""},
	}

	ttsModels := []ModelEntry{
		{Name: "Mimo TTS", Provider: "mimo", Model: env["MIMO_TTS_MODEL"], EnvKey: "MIMO_API_KEY", Available: env["MIMO_API_KEY"] != ""},
		{Name: "OpenAI TTS", Provider: "openai", Model: env["OPENAI_TTS_MODEL"], EnvKey: "OPENAI_API_KEY", Available: env["OPENAI_API_KEY"] != ""},
	}

	envVars := make(map[string]string)
	for _, m := range textModels {
		envVars[m.EnvKey] = env[m.EnvKey]
	}
	for _, m := range ttsModels {
		envVars[m.EnvKey] = env[m.EnvKey]
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"text":       textModels,
		"tts":        ttsModels,
		"multimodal": []ModelEntry{},
		"envVars":    envVars,
	})
}

func (s *Server) handleConfigPost(w http.ResponseWriter, req *http.Request) {
	var body struct {
		EnvVars map[string]string `json:"envVars"`
	}

	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}

	if body.EnvVars == nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "envVars object is required"})
		return
	}

	existing := loadEnv()
	for k, v := range body.EnvVars {
		existing[k] = v
	}

	lines := []string{
		"# OfferPilot 模型配置 (由弹窗自动生成)",
		"# 手动编辑也会保留",
		"",
	}

	groups := []struct {
		Label string
		Keys  []string
	}{
		{Label: "文本模型", Keys: []string{"ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL", "OPENAI_API_KEY", "OPENAI_BASE_URL", "OPENAI_MODEL", "DEEPSEEK_API_KEY", "DEEPSEEK_BASE_URL", "DEEPSEEK_MODEL"}},
		{Label: "TTS 语音合成", Keys: []string{"MIMO_API_KEY", "MIMO_BASE_URL", "MIMO_TTS_MODEL", "OPENAI_TTS_MODEL"}},
		{Label: "语音识别", Keys: []string{"MIMO_ASR_MODEL", "OPENAI_ASR_MODEL"}},
	}

	written := make(map[string]bool)
	for _, group := range groups {
		lines = append(lines, "# --- "+group.Label+" ---")
		for _, key := range group.Keys {
			if existing[key] != "" {
				lines = append(lines, key+"="+existing[key])
				written[key] = true
			}
		}
		lines = append(lines, "")
	}

	for k, v := range existing {
		if !written[k] && v != "" {
			lines = append(lines, k+"="+v)
		}
	}

	envPath := filepath.Join(os.Getenv("PROJECT_ROOT"), ".env")
	if envPath == ".env" {
		envPath = filepath.Join("..", ".env")
	}

	if err := os.WriteFile(envPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func loadEnv() map[string]string {
	env := make(map[string]string)
	envPath := filepath.Join(os.Getenv("PROJECT_ROOT"), ".env")
	if envPath == ".env" {
		envPath = filepath.Join("..", ".env")
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		return env
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eqIdx := strings.Index(line, "=")
		if eqIdx == -1 {
			continue
		}
		key := strings.TrimSpace(line[:eqIdx])
		val := strings.TrimSpace(line[eqIdx+1:])
		env[key] = val
	}

	return env
}

type DiagnosisRecord struct {
	ID        string `json:"id"`
	Timestamp int64  `json:"timestamp"`
	Dimension string `json:"dimension"`
	Score     int    `json:"score"`
	Question  string `json:"question"`
	SessionID string `json:"sessionId"`
}

type SM2State struct {
	Dimension   string
	EaseFactor  float64
	Interval    int
	Repetitions int
	NextReview  int64
}

var (
	diagnosisRecords []DiagnosisRecord
	sm2States        = make(map[string]SM2State)
	diagnosisMu      sync.Mutex
)

// recordDiagnosis is called by the record_diagnosis tool to persist a score.
func recordDiagnosis(sessionID, dimension string, score int, question string) {
	diagnosisMu.Lock()
	defer diagnosisMu.Unlock()

	score = max(1, min(10, score))

	record := DiagnosisRecord{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now().UnixMilli(),
		Dimension: dimension,
		Score:     score,
		Question:  question,
		SessionID: sessionID,
	}
	diagnosisRecords = append(diagnosisRecords, record)

	existing, ok := sm2States[dimension]
	if !ok {
		existing = SM2State{
			Dimension:   dimension,
			EaseFactor:  2.5,
			Interval:    1,
			Repetitions: 0,
			NextReview:  time.Now().UnixMilli(),
		}
	}

	quality := max(0, min(5, (score*5)/10))
	var interval int
	if quality >= 3 {
		if existing.Repetitions == 0 {
			interval = 1
		} else if existing.Repetitions == 1 {
			interval = 3
		} else {
			interval = int(float64(existing.Interval) * existing.EaseFactor)
		}
		existing.Repetitions++
	} else {
		existing.Repetitions = 0
		interval = 1
	}

	existing.EaseFactor = max(1.3, existing.EaseFactor+(0.1-float64(5-quality)*(0.08+float64(5-quality)*0.02)))
	existing.Interval = interval
	existing.NextReview = time.Now().UnixMilli() + int64(interval)*24*60*60*1000
	sm2States[dimension] = existing
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
		s.handleDiagnosisGet(w)
		return
	}

	if req.Method == http.MethodPost {
		s.handleDiagnosisPost(w, req)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleDiagnosisGet(w http.ResponseWriter) {
	diagnosisMu.Lock()
	defer diagnosisMu.Unlock()

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
		for _, r := range diagnosisRecords {
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

	totalAnswered := len(diagnosisRecords)
	avgScore := 0
	if totalAnswered > 0 {
		sum := 0
		for _, r := range diagnosisRecords {
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
	if len(diagnosisRecords) > 0 {
		start := len(diagnosisRecords) - 10
		if start < 0 {
			start = 0
		}
		recent = diagnosisRecords[start:]
	}

	type ReviewPriority struct {
		Dimension      string  `json:"dimension"`
		Urgency        int     `json:"urgency"`
		DaysUntilReview *int   `json:"daysUntilReview"`
	}

	reviewPriority := make([]ReviewPriority, 0)
	now := time.Now().UnixMilli()
	for _, dim := range dimensions {
		state, ok := sm2States[dim]
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
				Dimension:      dim,
				Urgency:        urgency,
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
	}

	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}

	if body.Dimension == "" || body.Score == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "dimension and score are required"})
		return
	}

	score := max(1, min(10, body.Score))

	diagnosisMu.Lock()
	defer diagnosisMu.Unlock()

	record := DiagnosisRecord{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now().UnixMilli(),
		Dimension: body.Dimension,
		Score:     score,
		Question:  body.Question,
	}
	diagnosisRecords = append(diagnosisRecords, record)

	existing, ok := sm2States[body.Dimension]
	if !ok {
		existing = SM2State{
			Dimension:   body.Dimension,
			EaseFactor:  2.5,
			Interval:    1,
			Repetitions: 0,
			NextReview:  time.Now().UnixMilli(),
		}
	}

	quality := max(0, min(5, (score/10)*5))
	var interval int
	if quality >= 3 {
		if existing.Repetitions == 0 {
			interval = 1
		} else if existing.Repetitions == 1 {
			interval = 3
		} else {
			interval = int(float64(existing.Interval) * existing.EaseFactor)
		}
		existing.Repetitions++
	} else {
		existing.Repetitions = 0
		interval = 1
	}

	existing.EaseFactor = max(1.3, existing.EaseFactor+(0.1-float64(5-quality)*(0.08+float64(5-quality)*0.02)))
	existing.Interval = interval
	existing.NextReview = time.Now().UnixMilli() + int64(interval)*24*60*60*1000
	sm2States[body.Dimension] = existing

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "record": record})
}

type InterviewSession struct {
	ID               string
	Questions        []string
	CurrentIndex     int
	State            string
	Transcript       []map[string]interface{}
	Defects          []map[string]interface{}
	QuestionStartTime int64
}

var (
	interviewSessions = make(map[string]*InterviewSession)
	interviewMu       sync.Mutex
)

var defaultQuestions = []string{
	"请介绍一下你在 Agent 方向的工作经历",
	"什么是 ReAct 模式？工程实现中需要注意什么？",
	"如何设计一个支持多 Provider 的 LLM 调用层？",
	"RAG 系统中 Chunk 策略有哪些选择？各自适合什么场景？",
	"说一个你优化系统性能的具体案例",
}

func (s *Server) handleInterview(w http.ResponseWriter, req *http.Request) {
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
		Action    string   `json:"action"`
		SessionID string   `json:"sessionId"`
		Answer    string   `json:"answer"`
		Questions []string `json:"questions"`
	}

	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}

	interviewMu.Lock()
	defer interviewMu.Unlock()

	if body.Action == "start" {
		id := fmt.Sprintf("%d", time.Now().UnixNano())
		qs := body.Questions
		if len(qs) == 0 {
			qs = defaultQuestions
		}

		session := &InterviewSession{
			ID:               id,
			Questions:        qs,
			CurrentIndex:     0,
			State:            "questioning",
			Transcript:       make([]map[string]interface{}, 0),
			Defects:          make([]map[string]interface{}, 0),
			QuestionStartTime: time.Now().UnixMilli(),
		}

		firstQuestion := qs[0]
		session.Transcript = append(session.Transcript, map[string]interface{}{
			"speaker":   "interviewer",
			"text":      firstQuestion,
			"timestamp": time.Now().UnixMilli(),
		})
		interviewSessions[id] = session

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sessionId": id,
			"question":  firstQuestion,
			"progress": map[string]int{"current": 1, "total": len(qs)},
		})
		return
	}

	session, ok := interviewSessions[body.SessionID]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Session not found"})
		return
	}

	if body.Action == "answer" {
		if body.Answer == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "answer is required"})
			return
		}

		elapsed := time.Now().UnixMilli() - session.QuestionStartTime
		session.Transcript = append(session.Transcript, map[string]interface{}{
			"speaker":   "candidate",
			"text":      body.Answer,
			"timestamp": time.Now().UnixMilli(),
		})

		defects := analyzeDefects(session.Questions[session.CurrentIndex], body.Answer, elapsed)
		session.Defects = append(session.Defects, defects...)
		session.CurrentIndex++

		hasNext := session.CurrentIndex < len(session.Questions)
		session.State = "answering"
		if !hasNext {
			session.State = "idle"
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"defects":  defects,
			"summary":  buildSummary(defects),
			"hasNext":  hasNext,
			"progress": map[string]int{"current": session.CurrentIndex, "total": len(session.Questions)},
		})
		return
	}

	if body.Action == "next" {
		if session.CurrentIndex >= len(session.Questions) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "No more questions", "done": true})
			return
		}

		question := session.Questions[session.CurrentIndex]
		session.State = "questioning"
		session.QuestionStartTime = time.Now().UnixMilli()
		session.Transcript = append(session.Transcript, map[string]interface{}{
			"speaker":   "interviewer",
			"text":      question,
			"timestamp": time.Now().UnixMilli(),
		})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"question": question,
			"progress": map[string]int{"current": session.CurrentIndex + 1, "total": len(session.Questions)},
		})
		return
	}

	if body.Action == "report" {
		defects := session.Defects
		bySeverity := map[string]int{"critical": 0, "moderate": 0, "minor": 0}
		for _, d := range defects {
			sev := d["severity"].(string)
			bySeverity[sev]++
		}

		typeCounts := make(map[string]int)
		for _, d := range defects {
			typ := d["type"].(string)
			typeCounts[typ]++
		}

		type IssueCount struct {
			Type  string `json:"type"`
			Count int    `json:"count"`
		}

		topIssues := make([]IssueCount, 0)
		for typ, count := range typeCounts {
			topIssues = append(topIssues, IssueCount{Type: typ, Count: count})
		}

		maxDefects := session.CurrentIndex * 4
		overallScore := max(1, 10-(len(defects)/max(maxDefects, 1))*7)

		delete(interviewSessions, session.ID)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"totalQuestions": session.CurrentIndex,
			"totalDefects":   len(defects),
			"bySeverity":     bySeverity,
			"topIssues":      topIssues,
			"overallScore":   overallScore,
			"transcript":     session.Transcript,
		})
		return
	}

	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": "Invalid action"})
}

func analyzeDefects(question, answer string, elapsedMs int64) []map[string]interface{} {
	defects := make([]map[string]interface{}, 0)
	idCounter := 0

	mkId := func() string {
		idCounter++
		return fmt.Sprintf("d%d-%d", time.Now().UnixMilli(), idCounter)
	}

	if len(answer) < 50 {
		defects = append(defects, map[string]interface{}{
			"id":          mkId(),
			"type":        "too_short",
			"severity":    "critical",
			"description": "回答过于简短，缺乏有效信息",
			"suggestion":  "至少展开 2-3 个要点，每个要点一句话",
		})
	}

	if len(answer) > 100 && !regexp.MustCompile(`[1-9一二三四五六七八九十][.、)）]`).MatchString(answer) && !strings.Contains(answer, "首先") && !strings.Contains(answer, "其次") {
		defects = append(defects, map[string]interface{}{
			"id":          mkId(),
			"type":        "no_structure",
			"severity":    "moderate",
			"description": "回答缺乏结构，一段到底",
			"suggestion":  "用\"第一…第二…第三…\"或\"首先…其次…最后…\"组织",
		})
	}

	if len(answer) > 80 && !strings.Contains(answer, "例如") && !strings.Contains(answer, "比如") && !strings.Contains(answer, "实际") && !strings.Contains(answer, "项目") {
		defects = append(defects, map[string]interface{}{
			"id":          mkId(),
			"type":        "missing_example",
			"severity":    "moderate",
			"description": "缺少具体案例支撑",
			"suggestion":  "加一句\"比如在我之前的项目中…\"增强说服力",
		})
	}

	vagueCount := len(regexp.MustCompile(`可能|大概|好像|一些|某些|差不多`).FindAllString(answer, -1))
	if vagueCount >= 3 {
		defects = append(defects, map[string]interface{}{
			"id":          mkId(),
			"type":        "too_vague",
			"severity":    "moderate",
			"description": fmt.Sprintf("模糊表述过多（%d 处）", vagueCount),
			"suggestion":  "用具体数字和明确说法替换\"大概\"\"可能\"",
		})
	}

	if len(answer) > 150 && !strings.Contains(answer, "因为") && !strings.Contains(answer, "原因") && !strings.Contains(answer, "本质") {
		defects = append(defects, map[string]interface{}{
			"id":          mkId(),
			"type":        "no_depth",
			"severity":    "minor",
			"description": "停留在表面描述，缺少原理分析",
			"suggestion":  "补充一句\"之所以这样做是因为…\"展示深度理解",
		})
	}

	fillerCount := len(regexp.MustCompile(`那个|就是说|嗯|额|然后就|对吧`).FindAllString(answer, -1))
	if fillerCount >= 3 {
		defects = append(defects, map[string]interface{}{
			"id":          mkId(),
			"type":        "filler_words",
			"severity":    "minor",
			"description": fmt.Sprintf("口头禅过多（%d 处）", fillerCount),
			"suggestion":  "放慢语速，用短暂停顿替代\"嗯\"\"那个\"",
		})
	}

	if elapsedMs > 15000 && len(answer) < 100 {
		defects = append(defects, map[string]interface{}{
			"id":          mkId(),
			"type":        "hesitation",
			"severity":    "minor",
			"description": "思考时间过长",
			"suggestion":  "先说\"这个问题我从X角度来回答\"争取思考时间",
		})
	}

	return defects
}

func buildSummary(defects []map[string]interface{}) string {
	if len(defects) == 0 {
		return "这道题回答不错，没有明显缺陷"
	}
	critical := 0
	for _, d := range defects {
		if d["severity"] == "critical" {
			critical++
		}
	}
	summary := fmt.Sprintf("发现 %d 个问题", len(defects))
	if critical > 0 {
		summary += fmt.Sprintf("（%d 个严重）", critical)
	}
	return summary
}

func (s *Server) handleMatch(w http.ResponseWriter, req *http.Request) {
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
		JD     string `json:"jd"`
		Resume string `json:"resume"`
	}

	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}

	if strings.TrimSpace(body.JD) == "" || strings.TrimSpace(body.Resume) == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "jd and resume are required"})
		return
	}

	jdKeywords := extractKeywords(body.JD)
	resumeKeywords := extractKeywords(body.Resume)

	matched := make([]string, 0)
	for _, kw := range jdKeywords {
		found := false
		for _, rk := range resumeKeywords {
			if strings.Contains(rk, kw) || strings.Contains(kw, rk) {
				found = true
				break
			}
		}
		if found {
			matched = append(matched, kw)
		}
	}

	missing := make([]string, 0)
	for _, kw := range jdKeywords {
		found := false
		for _, rk := range resumeKeywords {
			if strings.Contains(rk, kw) || strings.Contains(kw, rk) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, kw)
		}
	}

	score := 50
	if len(jdKeywords) > 0 {
		score = (len(matched) * 100) / len(jdKeywords)
	}

	level := detectLevel(body.JD)
	focus := detectFocus(body.JD)
	suggestions := generateSuggestions(missing, body.Resume)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"score":       score,
		"matched":     matched,
		"missing":     missing,
		"suggestions": suggestions,
		"level":       level,
		"focus":       focus,
	})
}

func extractKeywords(text string) []string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`TypeScript|JavaScript|Python|Go|Rust|Java|C\+\+`),
		regexp.MustCompile(`React|Vue|Angular|Next\.?js|Node\.?js|Express`),
		regexp.MustCompile(`Docker|Kubernetes|K8s|CI\/CD|GitHub Actions`),
		regexp.MustCompile(`PostgreSQL|MySQL|Redis|MongoDB|SQLite|Elasticsearch`),
		regexp.MustCompile(`LLM|GPT|Claude|Agent|RAG|embedding|向量`),
		regexp.MustCompile(`分布式|微服务|gRPC|REST|GraphQL|WebSocket|SSE`),
		regexp.MustCompile(`TDD|单元测试|E2E|集成测试|自动化测试`),
		regexp.MustCompile(`Webpack|Vite|ESBuild|Tailwind|shadcn`),
		regexp.MustCompile(`AWS|GCP|Azure|阿里云|腾讯云`),
		regexp.MustCompile(`机器学习|深度学习|NLP|CV|MLOps|训练|微调`),
		regexp.MustCompile(`Milvus|Qdrant|Pinecone|Faiss|向量数据库`),
		regexp.MustCompile(`Prompt|CoT|ReAct|Tool Use|Function Calling`),
		regexp.MustCompile(`架构设计|系统设计|高可用|高并发|性能优化`),
		regexp.MustCompile(`数据处理|ETL|数据管道|Spark|Flink`),
	}

	keywords := make(map[string]bool)
	for _, pattern := range patterns {
		matches := pattern.FindAllString(text, -1)
		for _, m := range matches {
			keywords[strings.ToLower(strings.TrimSpace(m))] = true
		}
	}

	cnMatches := regexp.MustCompile(`[一-鿿]{2,6}(?:系统|架构|服务|引擎|平台|框架|协议|模型|算法|能力)`).FindAllString(text, -1)
	for _, m := range cnMatches {
		keywords[m] = true
	}

	result := make([]string, 0, len(keywords))
	for k := range keywords {
		result = append(result, k)
	}
	return result
}

func detectLevel(jd string) string {
	if regexp.MustCompile(`[5五]年以上|资深|高级|P[67]`).MatchString(jd) {
		return "高级工程师 (P6-P7)"
	}
	if regexp.MustCompile(`[3三]年以上|中级|P5`).MatchString(jd) {
		return "中级工程师 (P5)"
	}
	if regexp.MustCompile(`[8八]年以上|专家|架构师|P[89]`).MatchString(jd) {
		return "专家/架构师 (P8+)"
	}
	return "工程师"
}

func detectFocus(jd string) []string {
	areas := make([]string, 0)
	if regexp.MustCompile(`Agent|LLM|大模型|GPT|Claude`).MatchString(jd) {
		areas = append(areas, "AI/LLM 工程")
	}
	if regexp.MustCompile(`架构|系统设计|分布式`).MatchString(jd) {
		areas = append(areas, "系统架构")
	}
	if regexp.MustCompile(`全栈|前端|后端|Web`).MatchString(jd) {
		areas = append(areas, "全栈开发")
	}
	if regexp.MustCompile(`RAG|检索|知识库|向量`).MatchString(jd) {
		areas = append(areas, "RAG/检索")
	}
	if regexp.MustCompile(`数据|ETL|管道|分析`).MatchString(jd) {
		areas = append(areas, "数据工程")
	}
	if len(areas) == 0 {
		areas = append(areas, "软件工程")
	}
	return areas
}

func generateSuggestions(missing []string, resume string) []string {
	suggestions := make([]string, 0)

	if len(missing) > 5 {
		suggestions = append(suggestions, "JD 要求的技术栈覆盖不足，建议在项目经历中补充相关技术的使用经验")
	}

	hasDocker := false
	hasDistributed := false
	hasAgent := false
	hasTest := false
	for _, kw := range missing {
		if strings.Contains(kw, "docker") || strings.Contains(kw, "k8s") || strings.Contains(kw, "kubernetes") || strings.Contains(kw, "容器") {
			hasDocker = true
		}
		if strings.Contains(kw, "分布式") || strings.Contains(kw, "高并发") || strings.Contains(kw, "高可用") {
			hasDistributed = true
		}
		if strings.Contains(kw, "agent") || strings.Contains(kw, "llm") || strings.Contains(kw, "大模型") || strings.Contains(kw, "rag") {
			hasAgent = true
		}
		if strings.Contains(kw, "测试") || strings.Contains(kw, "tdd") || strings.Contains(kw, "e2e") {
			hasTest = true
		}
	}

	if hasDocker {
		suggestions = append(suggestions, "补充容器化/部署相关经验，即使只是 Docker 单机部署也值得提及")
	}
	if hasDistributed {
		suggestions = append(suggestions, "在项目中突出系统规模（QPS、数据量、节点数），体现分布式思维")
	}
	if hasAgent {
		suggestions = append(suggestions, "突出 AI/LLM 相关实践，包括 Prompt 工程、RAG 搭建、Agent 开发")
	}
	if hasTest {
		suggestions = append(suggestions, "补充测试实践：测试覆盖率、TDD 经验、CI 自动化")
	}

	if !regexp.MustCompile(`\d+%|\d+ms|\d+QPS|\d+万`).MatchString(resume) {
		suggestions = append(suggestions, "简历中缺少量化数据，建议每个项目至少有 1-2 个数字指标")
	}

	if len(suggestions) == 0 {
		suggestions = append(suggestions, "匹配度良好，建议进一步强化最核心的 2-3 个技术点的深度描述")
	}

	if len(suggestions) > 5 {
		suggestions = suggestions[:5]
	}
	return suggestions
}

func (s *Server) handleResume(w http.ResponseWriter, req *http.Request) {
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
		Content string `json:"content"`
	}

	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}

	if strings.TrimSpace(body.Content) == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "content is required"})
		return
	}

	sections := splitSections(body.Content)
	diagnosis := make([]map[string]interface{}, 0)
	for _, section := range sections {
		diagnosis = append(diagnosis, analyzeSection(section))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"diagnosis": diagnosis})
}

func splitSections(content string) []map[string]string {
	lines := strings.Split(content, "\n")
	sections := make([]map[string]string, 0)
	var current map[string]string

	for _, line := range lines {
		match := regexp.MustCompile(`^#{1,3}\s+(.+)`).FindStringSubmatch(line)
		if match != nil {
			if current != nil {
				sections = append(sections, current)
			}
			current = map[string]string{"title": match[1], "text": ""}
		} else if current != nil {
			if current["text"] == "" {
				current["text"] = line
			} else {
				current["text"] += "\n" + line
			}
		} else if strings.TrimSpace(line) != "" {
			current = map[string]string{"title": "项目经历", "text": line}
		}
	}

	if current != nil {
		sections = append(sections, current)
	}

	if len(sections) == 0 {
		paragraphs := regexp.MustCompile(`\n{2,}`).Split(content, -1)
		for i, p := range paragraphs {
			if strings.TrimSpace(p) != "" {
				sections = append(sections, map[string]string{"title": fmt.Sprintf("段落 %d", i+1), "text": strings.TrimSpace(p)})
			}
		}
	}

	return sections
}

func analyzeSection(section map[string]string) map[string]interface{} {
	title := section["title"]
	text := section["text"]
	issues := make([]string, 0)
	suggestions := make([]string, 0)
	score := 8

	if len(text) < 30 {
		issues = append(issues, "内容过少")
		suggestions = append(suggestions, "补充技术栈、成果和具体数据")
		score -= 2
	}

	if !regexp.MustCompile(`\d+`).MatchString(text) {
		issues = append(issues, "缺少量化数据")
		suggestions = append(suggestions, "加入性能指标：延迟、QPS、成功率、覆盖人数等")
		score -= 1
	}

	if !regexp.MustCompile(`[结果成果效果提升降低优化]`).MatchString(text) && len(text) > 50 {
		issues = append(issues, "未体现成果")
		suggestions = append(suggestions, "用 STAR 结构结尾加上 Result（成果）")
		score -= 1
	}

	if !regexp.MustCompile(`[选择|设计|架构|方案]`).MatchString(text) && len(text) > 80 {
		issues = append(issues, "未体现技术决策")
		suggestions = append(suggestions, "描述为什么选择该技术方案，体现判断力")
		score -= 1
	}

	if len(text) > 60 && !strings.Contains(text, "负责") && !strings.Contains(text, "主导") && !strings.Contains(text, "我") {
		issues = append(issues, "未突出个人贡献")
		suggestions = append(suggestions, "明确个人角色：\"我负责...\"、\"我主导了...\"")
		score -= 1
	}

	buzzwords := len(regexp.MustCompile(`精通|熟悉|了解|掌握`).FindAllString(text, -1))
	if buzzwords >= 3 {
		issues = append(issues, "技能描述太泛")
		suggestions = append(suggestions, "用项目经验佐证技能水平，而非堆砌\"精通/熟悉\"")
		score -= 1
	}

	if len(issues) == 0 {
		suggestions = append(suggestions, "继续保持，可适当补充更多量化数据")
	}

	return map[string]interface{}{
		"section":     title,
		"score":       max(3, min(10, score)),
		"issues":      issues,
		"suggestions": suggestions,
	}
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

	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}

	url := strings.TrimSpace(body.URL)
	if url == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "URL is required"})
		return
	}

	if !strings.HasPrefix(url, "http") {
		url = "https://" + url
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
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

	htmlData, err := io.ReadAll(resp.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to read URL content"})
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
	json.NewEncoder(w).Encode(map[string]interface{}{"text": text, "source": url})
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

func (s *Server) handleStaticOrSPA(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/" {
		data, err := webFiles.ReadFile("web/index.html")
		if err != nil {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
		return
	}

	filePath := filepath.Join("web", req.URL.Path)
	data, err := webFiles.ReadFile(filePath)
	if err != nil {
		data, err = webFiles.ReadFile("web/index.html")
		if err != nil {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
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

	return data
}

func getContentType(ext string) string {
	switch ext {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
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

