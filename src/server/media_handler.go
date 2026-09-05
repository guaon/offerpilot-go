package server

import (
	"encoding/json"
	"io"
	"net/http"

	"MyOfferPilot/src/logger"
	"MyOfferPilot/src/realtime"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
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
		if isRequestTooLarge(err) {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "request_too_large", "Request body is too large", false)
			return
		}
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

	var request struct {
		Text   string `json:"text"`
		Voice  string `json:"voice"`
		Format string `json:"format"`
	}
	if !decodeJSON(w, req, &request) {
		return
	}
	if request.Text == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "text is required"})
		return
	}

	result, err := realtime.SynthesizeSpeech(realtime.SynthesizeSpeechInput{
		Text:   request.Text,
		Voice:  request.Voice,
		Format: request.Format,
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
