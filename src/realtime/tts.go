package realtime

import (
	"fmt"
	"net/url"
)

type TTSEngine struct {
	config TTSConfig
}

func NewTTSEngine(config *TTSConfig) *TTSEngine {
	cfg := TTSConfig{
		Provider: TTSProviderBrowser,
		Voice:    "zh-CN-XiaoxiaoNeural",
		Speed:    1.0,
		Language: "zh-CN",
	}

	if config != nil {
		if config.Provider != "" {
			cfg.Provider = config.Provider
		}
		if config.Voice != "" {
			cfg.Voice = config.Voice
		}
		if config.Speed > 0 {
			cfg.Speed = config.Speed
		}
		if config.Language != "" {
			cfg.Language = config.Language
		}
	}

	return &TTSEngine{config: cfg}
}

func (e *TTSEngine) Synthesize(text string) TTSOutput {
	switch e.config.Provider {
	case TTSProviderEdgeTTS:
		return e.edgeTTS(text)
	case TTSProviderOpenAITTS:
		return e.openaiTTS(text)
	default:
		return e.browserTTS(text)
	}
}

func (e *TTSEngine) browserTTS(text string) TTSOutput {
	return TTSOutput{
		Text: text,
		SSML: e.buildSSML(text),
	}
}

func (e *TTSEngine) edgeTTS(text string) TTSOutput {
	return TTSOutput{
		Text:     text,
		SSML:     e.buildSSML(text),
		AudioURL: "/api/tts/edge?text=" + url.QueryEscape(text) + "&voice=" + url.QueryEscape(e.config.Voice),
	}
}

func (e *TTSEngine) openaiTTS(text string) TTSOutput {
	return TTSOutput{
		Text:     text,
		AudioURL: "/api/tts/openai?text=" + url.QueryEscape(text),
	}
}

func (e *TTSEngine) buildSSML(text string) string {
	return fmt.Sprintf(`<speak version="1.0" xmlns="http://www.w3.org/2001/10/synthesis" xml:lang="%s">
  <voice name="%s">
    <prosody rate="%f">
      %s
    </prosody>
  </voice>
</speak>`, e.config.Language, e.config.Voice, e.config.Speed, text)
}