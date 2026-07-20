package server

import (
	"context"
	"embed"
	"encoding/json"
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
		newSession := appInst.SessionManager.Create("")
		sessionID = newSession.ID
	}

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

	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.validateAuth(req) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	session := s.app.SessionManager.Create("")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"sessionId": session.ID})
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

	err := req.ParseMultipartForm(10 << 20)
	if err != nil {
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

	if strings.HasSuffix(filename, ".pdf") {
		text := extractTextFromPDF(data)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"text": text, "pages": nil, "format": "pdf"})
		return
	}

	if strings.HasSuffix(filename, ".docx") || strings.HasSuffix(filename, ".doc") {
		text := extractTextFromDocx(data)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"text": text, "pages": nil, "format": "docx"})
		return
	}

	if strings.HasSuffix(filename, ".tex") {
		text := stripLatex(string(data))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"text": text, "pages": nil, "format": "tex"})
		return
	}

	if strings.HasSuffix(filename, ".txt") || strings.HasSuffix(filename, ".md") {
		text := string(data)
		format := "txt"
		if strings.HasSuffix(filename, ".md") {
			format = "md"
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"text": text, "pages": nil, "format": format})
		return
	}

	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": "Unsupported file format"})
}

func extractTextFromPDF(data []byte) string {
	text := string(data)
	text = regexp.MustCompile(`/T\(([^)]+)\)`).ReplaceAllString(text, "$1 ")
	text = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`[^\x20-\x7E\u4E00-\u9FFF\n]`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func extractTextFromDocx(data []byte) string {
	text := string(data)
	text = regexp.MustCompile(`<w:t>([^<]+)</w:t>`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(text, "")
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