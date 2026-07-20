package realtime

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const (
	defaultBaseURL     = "https://api.xiaomimimo.com/v1"
	defaultASRModel   = "mimo-v2.5-asr"
	defaultTTSModel   = "mimo-v2.5-tts"
	defaultTTSVoice   = "alloy"
)

type TranscribeAudioInput struct {
	Audio       []byte
	FileName    string
	ContentType string
}

type TranscribeAudioOutput struct {
	Text string `json:"text"`
}

type SynthesizeSpeechInput struct {
	Text   string
	Voice  string
	Format string
}

type SynthesizeSpeechOutput struct {
	Audio       []byte `json:"-"`
	ContentType string `json:"contentType"`
}

type mimoConfig struct {
	apiKey    string
	baseUrl   string
	asrModel  string
	ttsModel  string
	ttsVoice  string
}

func getConfig() (*mimoConfig, error) {
	apiKey := os.Getenv("MIMO_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("MIMO_API_KEY is not configured")
	}

	baseUrl := os.Getenv("MIMO_BASE_URL")
	if baseUrl == "" {
		baseUrl = defaultBaseURL
	}
	baseUrl = strings.TrimSuffix(baseUrl, "/")

	return &mimoConfig{
		apiKey:    apiKey,
		baseUrl:   baseUrl,
		asrModel:  os.Getenv("MIMO_ASR_MODEL"),
		ttsModel:  os.Getenv("MIMO_TTS_MODEL"),
		ttsVoice:  os.Getenv("MIMO_TTS_VOICE"),
	}, nil
}

func TranscribeAudio(input TranscribeAudioInput) (*TranscribeAudioOutput, error) {
	config, err := getConfig()
	if err != nil {
		return nil, err
	}

	fileName := input.FileName
	if fileName == "" {
		fileName = "answer.webm"
	}

	contentType := input.ContentType
	if contentType == "" {
		contentType = "audio/webm"
	}

	if !isSupportedAsrAudio(contentType, fileName) {
		return nil, fmt.Errorf("Mimo ASR only supports wav or mp3 audio. Please record again or upload a .wav/.mp3 file.")
	}

	dataUrl := fmt.Sprintf("data:%s;base64,%s", normalizeAudioMime(contentType, fileName), base64.StdEncoding.EncodeToString(input.Audio))

	reqBody := map[string]interface{}{
		"model": config.asrModel,
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{
						"type": "input_audio",
						"input_audio": map[string]interface{}{
							"data": dataUrl,
						},
					},
				},
			},
		},
		"asr_options": map[string]interface{}{
			"language": os.Getenv("MIMO_ASR_LANGUAGE"),
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/chat/completions", config.baseUrl)
	resp, err := http.Post(url, "application/json", strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("Mimo ASR request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Mimo ASR failed: %d %s", resp.StatusCode, string(body))
	}

	var result struct {
		Text    *string `json:"text"`
		Choices []struct {
			Message *struct {
				Content interface{} `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	text := ""
	if result.Text != nil {
		text = *result.Text
	} else if len(result.Choices) > 0 && result.Choices[0].Message != nil {
		text = extractText(result.Choices[0].Message.Content)
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("Mimo ASR returned empty transcript")
	}

	return &TranscribeAudioOutput{Text: text}, nil
}

func SynthesizeSpeech(input SynthesizeSpeechInput) (*SynthesizeSpeechOutput, error) {
	config, err := getConfig()
	if err != nil {
		return nil, err
	}

	format := input.Format
	if format == "" {
		format = "wav"
	}

	voice := input.Voice
	if voice == "" {
		voice = config.ttsVoice
	}

	reqBody := map[string]interface{}{
		"model": config.ttsModel,
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": "用自然、清晰、适合中文面试反馈的语气朗读。",
			},
			map[string]interface{}{
				"role":    "assistant",
				"content": input.Text,
			},
		},
		"audio": map[string]interface{}{
			"format": format,
			"voice":  voice,
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/chat/completions", config.baseUrl)
	resp, err := http.Post(url, "application/json", strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("Mimo TTS request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Mimo TTS failed: %d %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message *struct {
				Audio *struct {
					Data *string `json:"data"`
				} `json:"audio"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var audioBase64 string
	if len(result.Choices) > 0 && result.Choices[0].Message != nil && result.Choices[0].Message.Audio != nil && result.Choices[0].Message.Audio.Data != nil {
		audioBase64 = *result.Choices[0].Message.Audio.Data
	}

	if audioBase64 == "" {
		return nil, fmt.Errorf("Mimo TTS returned empty audio")
	}

	audio, err := base64.StdEncoding.DecodeString(audioBase64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode audio: %w", err)
	}

	contentType := fmt.Sprintf("audio/%s", format)
	if format == "wav" {
		contentType = "audio/wav"
	}

	return &SynthesizeSpeechOutput{
		Audio:       audio,
		ContentType: contentType,
	}, nil
}

func isSupportedAsrAudio(contentType, fileName string) bool {
	mime := strings.ToLower(strings.Split(contentType, ";")[0])
	name := strings.ToLower(fileName)
	return mime == "audio/wav" || mime == "audio/x-wav" || mime == "audio/mpeg" || mime == "audio/mp3" ||
		strings.HasSuffix(name, ".wav") || strings.HasSuffix(name, ".mp3")
}

func normalizeAudioMime(contentType, fileName string) string {
	mime := strings.ToLower(strings.Split(contentType, ";")[0])
	if mime == "audio/x-wav" || strings.HasSuffix(strings.ToLower(fileName), ".wav") {
		return "audio/wav"
	}
	if mime == "audio/mp3" || strings.HasSuffix(strings.ToLower(fileName), ".mp3") {
		return "audio/mpeg"
	}
	return mime
}

func extractText(content interface{}) string {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return strings.TrimSpace(v)
	case []interface{}:
		var text strings.Builder
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok {
					text.WriteString(t)
				}
			}
		}
		return strings.TrimSpace(text.String())
	default:
		return ""
	}
}