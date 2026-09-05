package server

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"MyOfferPilot/src/logger"
	resumediagnosis "MyOfferPilot/src/resume"
	tool "MyOfferPilot/src/tools"
)

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
	if !decodeJSON(w, req, &body) {
		return
	}
	if strings.TrimSpace(body.JD) == "" || strings.TrimSpace(body.Resume) == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "jd and resume are required"})
		return
	}

	jdKeywords := tool.ExtractKeywords(body.JD)
	resumeKeywords := tool.ExtractKeywords(body.Resume)
	matched := matchingKeywords(jdKeywords, resumeKeywords)
	missing := missingKeywords(jdKeywords, resumeKeywords)
	score := 50
	if len(jdKeywords) > 0 {
		score = len(matched) * 100 / len(jdKeywords)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"score":       score,
		"matched":     matched,
		"missing":     missing,
		"suggestions": tool.GenerateSuggestions(missing, body.Resume),
		"level":       tool.DetectLevel(body.JD),
		"focus":       tool.DetectFocus(body.JD),
	})
}

func matchingKeywords(required, actual []string) []string {
	matched := make([]string, 0)
	for _, keyword := range required {
		if containsRelatedKeyword(actual, keyword) {
			matched = append(matched, keyword)
		}
	}
	return matched
}

func missingKeywords(required, actual []string) []string {
	missing := make([]string, 0)
	for _, keyword := range required {
		if !containsRelatedKeyword(actual, keyword) {
			missing = append(missing, keyword)
		}
	}
	return missing
}

func containsRelatedKeyword(keywords []string, target string) bool {
	for _, keyword := range keywords {
		if strings.Contains(keyword, target) || strings.Contains(target, keyword) {
			return true
		}
	}
	return false
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

	req.Body = http.MaxBytesReader(w, req.Body, 12<<20)
	var body resumediagnosis.Request
	if !decodeJSON(w, req, &body) {
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "resume content is required", false)
		return
	}
	if len(body.Images) > 3 {
		writeAPIError(w, http.StatusBadRequest, "too_many_images", "at most three resume page images are allowed", false)
		return
	}
	for _, image := range body.Images {
		if len(image) > 3<<20 || (!strings.HasPrefix(image, "data:image/jpeg;base64,") && !strings.HasPrefix(image, "data:image/png;base64,")) {
			writeAPIError(w, http.StatusBadRequest, "invalid_image", "resume images must be bounded JPEG or PNG data URLs", false)
			return
		}
	}
	if s.app == nil || s.app.QueryEngine == nil || !s.app.QueryEngine.Available() {
		writeAPIError(w, http.StatusServiceUnavailable, "diagnostician_unavailable", "resume diagnostician is unavailable", true)
		return
	}
	if len(body.Images) > 0 {
		if os.Getenv("OPENAI_API_KEY") == "" {
			writeAPIError(w, http.StatusServiceUnavailable, "vision_model_unavailable", "multimodal diagnosis requires an OpenAI-compatible vision model", false)
			return
		}
		body.Model = os.Getenv("OPENAI_MODEL")
		if body.Model == "" {
			body.Model = "gpt-4o"
		}
	}

	diagnostician := resumediagnosis.NewDiagnostician(s.app.QueryEngine, 120*time.Second)
	result, err := diagnostician.Diagnose(req.Context(), body)
	if err != nil {
		logger.DefaultLogger.Warn("resume diagnosis failed", map[string]interface{}{"error": err.Error()})
		writeAPIError(w, http.StatusServiceUnavailable, "resume_diagnosis_failed", "Multimodal resume diagnosis is temporarily unavailable", true)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
