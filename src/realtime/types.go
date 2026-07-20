package realtime

type RealtimeSessionState string

const (
	RealtimeSessionIdle        RealtimeSessionState = "idle"
	RealtimeSessionQuestioning RealtimeSessionState = "questioning"
	RealtimeSessionAnswering   RealtimeSessionState = "answering"
	RealtimeSessionAnalyzing   RealtimeSessionState = "analyzing"
	RealtimeSessionSpeaking    RealtimeSessionState = "speaking"
)

type SpeakerType string

const (
	SpeakerInterviewer SpeakerType = "interviewer"
	SpeakerCandidate   SpeakerType = "candidate"
)

type DefectSeverity string

const (
	DefectSeverityMinor    DefectSeverity = "minor"
	DefectSeverityModerate DefectSeverity = "moderate"
	DefectSeverityCritical DefectSeverity = "critical"
)

type DefectType string

const (
	DefectTypeTooVague       DefectType = "too_vague"
	DefectTypeMissingExample DefectType = "missing_example"
	DefectTypeNoStructure    DefectType = "no_structure"
	DefectTypeFactualError   DefectType = "factual_error"
	DefectTypeTooShort       DefectType = "too_short"
	DefectTypeOffTopic       DefectType = "off_topic"
	DefectTypeNoDepth        DefectType = "no_depth"
	DefectTypeFillerWords    DefectType = "filler_words"
	DefectTypeHesitation     DefectType = "hesitation"
)

type RealtimeSession struct {
	ID              string               `json:"id"`
	State           RealtimeSessionState `json:"state"`
	CurrentQuestion string               `json:"currentQuestion"`
	QuestionsAsked  int                  `json:"questionsAsked"`
	TotalQuestions  int                  `json:"totalQuestions"`
	Transcript      []TranscriptEntry    `json:"transcript"`
	Defects         []DefectEntry        `json:"defects"`
}

type TranscriptEntry struct {
	Speaker   SpeakerType `json:"speaker"`
	Text      string      `json:"text"`
	Timestamp int64       `json:"timestamp"`
	DurationMs int64       `json:"durationMs,omitempty"`
}

type DefectEntry struct {
	ID          string         `json:"id"`
	Type        DefectType     `json:"type"`
	Severity    DefectSeverity `json:"severity"`
	Description string         `json:"description"`
	Timestamp   int64          `json:"timestamp"`
	Suggestion  string         `json:"suggestion"`
}

type TTSProvider string //语音合成

const (
	TTSProviderBrowser   TTSProvider = "browser"
	TTSProviderEdgeTTS   TTSProvider = "edge-tts"
	TTSProviderOpenAITTS TTSProvider = "openai-tts"
)

type TTSConfig struct {
	Provider TTSProvider `json:"provider"`
	Voice    string      `json:"voice,omitempty"`
	Speed    float64     `json:"speed,omitempty"`
	Language string      `json:"language,omitempty"`
}

type STTProvider string //语音识别

const (
	STTProviderBrowser STTProvider = "browser"
	STTProviderWhisper STTProvider = "whisper"
	STTProviderFunASR  STTProvider = "funasr"
)

type STTConfig struct {
	Provider STTProvider `json:"provider"`
	Language string      `json:"language,omitempty"`
	Realtime bool        `json:"realtime,omitempty"` //是否为实时模式
}

type TTSOutput struct {
	Text     string `json:"text"`
	AudioURL string `json:"audioUrl,omitempty"`
	SSML     string `json:"ssml,omitempty"`
}
